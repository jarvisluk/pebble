import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  ResolvedLang,
  WorkspaceFileContent,
  WorkspaceFileEntry,
  WorkspaceListResult,
} from '../types';
import {
  fetchWorkspaceFileContent,
  fetchWorkspaceFiles,
  requestWorkspaceFileTicket,
  workspaceFileRawURL,
} from '../lib/api';
import { translate } from '../lib/i18n';
import { renderMarkdownHtml } from '../lib/markdown';
import { formatStamp } from '../lib/theme';
import { formatFileSize } from './ChatView';

interface WorkspaceFilesPaneProps {
  botId: string;
  initialPath?: string;
  openSeq?: number;
  lang: ResolvedLang;
  onQuotePath: (relPath: string) => void;
  onToast: (msg: string) => void;
}

export const WorkspaceFilesPane: React.FC<WorkspaceFilesPaneProps> = ({
  botId,
  initialPath = '',
  openSeq = 0,
  lang,
  onQuotePath,
  onToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [dirPath, setDirPath] = useState('');
  const [showHidden, setShowHidden] = useState(false);
  const [listData, setListData] = useState<WorkspaceListResult | null>(null);
  const [listLoading, setListLoading] = useState(false);
  const [listErr, setListErr] = useState('');

  const [selectedPath, setSelectedPath] = useState('');
  const [preview, setPreview] = useState<WorkspaceFileContent | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewErr, setPreviewErr] = useState('');
  const [imageUrl, setImageUrl] = useState('');

  const listSeqRef = useRef(0);
  const previewSeqRef = useRef(0);

  const loadDir = useCallback(
    async (targetPath: string, hidden: boolean) => {
      if (!botId) return;
      const seq = ++listSeqRef.current;
      setListLoading(true);
      setListErr('');
      try {
        const res = await fetchWorkspaceFiles(botId, targetPath, hidden);
        if (seq !== listSeqRef.current) return;
        setListData(res);
        setDirPath(res.path || '');
      } catch (e) {
        if (seq !== listSeqRef.current) return;
        setListErr(e instanceof Error ? e.message : t('opFailed'));
      } finally {
        if (seq === listSeqRef.current) setListLoading(false);
      }
    },
    [botId]
  );

  const openFile = useCallback(
    async (relPath: string) => {
      if (!botId || !relPath) return;
      const seq = ++previewSeqRef.current;
      setSelectedPath(relPath);
      setPreviewLoading(true);
      setPreviewErr('');
      setImageUrl('');
      try {
        const doc = await fetchWorkspaceFileContent(botId, relPath);
        if (seq !== previewSeqRef.current) return;
        setPreview(doc);
        if (doc.kind === 'image') {
          const tk = await requestWorkspaceFileTicket(botId, relPath);
          if (seq !== previewSeqRef.current) return;
          setImageUrl(workspaceFileRawURL(botId, relPath, tk.ticket, false));
        }
      } catch (e) {
        if (seq !== previewSeqRef.current) return;
        setPreview(null);
        setPreviewErr(e instanceof Error ? e.message : t('opFailed'));
      } finally {
        if (seq === previewSeqRef.current) setPreviewLoading(false);
      }
    },
    [botId]
  );

  useEffect(() => {
    setSelectedPath('');
    setPreview(null);
    setPreviewErr('');
    setImageUrl('');
    if (!botId) return;
    if (initialPath) {
      const idx = initialPath.lastIndexOf('/');
      const parent = idx >= 0 ? initialPath.slice(0, idx) : '';
      void loadDir(parent, showHidden);
      void openFile(initialPath);
    } else {
      void loadDir('', showHidden);
    }
  }, [botId, initialPath, openSeq]);

  const handleToggleHidden = () => {
    const next = !showHidden;
    setShowHidden(next);
    void loadDir(dirPath, next);
  };

  const handleEntryClick = (ent: WorkspaceFileEntry) => {
    if (ent.isDir) {
      void loadDir(ent.path, showHidden);
    } else {
      void openFile(ent.path);
    }
  };

  const handleDownload = async (relPath: string, fileName: string) => {
    try {
      const tk = await requestWorkspaceFileTicket(botId, relPath);
      const url = workspaceFileRawURL(botId, relPath, tk.ticket, true);
      const a = document.createElement('a');
      a.href = url;
      a.download = fileName;
      document.body.appendChild(a);
      a.click();
      a.remove();
    } catch (e) {
      onToast(e instanceof Error ? e.message : t('opFailed'));
    }
  };

  const handleCopyPath = async (relPath: string) => {
    try {
      await navigator.clipboard.writeText(relPath);
      onToast(t('pathCopied'));
    } catch {
      onToast(relPath);
    }
  };

  const segments = dirPath ? dirPath.split('/').filter(Boolean) : [];
  const hasPreview = Boolean(selectedPath);

  return (
    <div className={`ws-explorer${hasPreview ? ' has-preview' : ''}`}>
      <div className="ws-tree-col">
        <div className="ws-toolbar">
          <div className="ws-crumbs" role="navigation" aria-label={t('workspaceFiles')}>
            <button
              type="button"
              className={`ws-crumb${dirPath === '' ? ' cur' : ''}`}
              onClick={() => void loadDir('', showHidden)}
            >
              {t('rootWorkspace')}
            </button>
            {segments.map((seg, i) => {
              const segPath = segments.slice(0, i + 1).join('/');
              const isLast = i === segments.length - 1;
              return (
                <React.Fragment key={segPath}>
                  <span className="ws-crumb-sep">/</span>
                  <button
                    type="button"
                    className={`ws-crumb${isLast ? ' cur' : ''}`}
                    onClick={() => void loadDir(segPath, showHidden)}
                  >
                    {seg}
                  </button>
                </React.Fragment>
              );
            })}
          </div>
          <div className="ws-toolbar-acts">
            <button
              type="button"
              className={`ghost ws-icon-btn${showHidden ? ' active' : ''}`}
              aria-label={t('showHiddenFiles')}
              title={t('showHiddenFiles')}
              onClick={handleToggleHidden}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
                <circle cx="12" cy="12" r="3" />
              </svg>
            </button>
            <button
              type="button"
              className="ghost ws-icon-btn"
              aria-label={t('refreshFiles')}
              title={t('refreshFiles')}
              onClick={() => void loadDir(dirPath, showHidden)}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 12a9 9 0 1 1-2.64-6.36" />
                <path d="M21 3v6h-6" />
              </svg>
            </button>
          </div>
        </div>

        <div className="ws-list">
          {listLoading && !listData ? (
            <div className="ws-empty">{t('loading')}</div>
          ) : listErr ? (
            <div className="ws-empty ws-err">
              <div>{listErr}</div>
              <button
                type="button"
                className="btn ws-retry-btn"
                onClick={() => void loadDir(dirPath, showHidden)}
              >
                {t('retry')}
              </button>
            </div>
          ) : !listData?.entries?.length ? (
            <div className="ws-empty">{t('emptyFolder')}</div>
          ) : (
            <>
              {dirPath !== '' && (
                <button
                  type="button"
                  className="ws-row"
                  onClick={() => void loadDir(listData.parent || '', showHidden)}
                >
                  <span className="ws-row-ico">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M15 18l-6-6 6-6" />
                    </svg>
                  </span>
                  <span className="ws-row-main">
                    <span className="ws-row-name">..</span>
                  </span>
                </button>
              )}
              {listData.entries.map((ent) => {
                const active = !ent.isDir && ent.path === selectedPath;
                return (
                  <button
                    key={ent.path}
                    type="button"
                    className={`ws-row${active ? ' sel' : ''}${ent.isShared ? ' is-shared' : ''}`}
                    onClick={() => handleEntryClick(ent)}
                  >
                    <span className="ws-row-ico">
                      {ent.isDir ? (
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
                        </svg>
                      ) : ent.mimeType?.startsWith('image/') ? (
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                          <rect x="3" y="3" width="18" height="18" rx="2" />
                          <circle cx="8.5" cy="8.5" r="1.5" />
                          <path d="m21 15-5-5L5 21" />
                        </svg>
                      ) : /\.(go|ts|tsx|js|jsx|py|rs|swift|json|yaml|yml|toml|sh|sql|css|html)$/i.test(ent.name) ? (
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                          <path d="m8 9-4 3 4 3M16 9l4 3-4 3M13 6l-2 12" />
                        </svg>
                      ) : (
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                          <path d="M14 3v5h5" />
                        </svg>
                      )}
                    </span>
                    <span className="ws-row-main">
                      <span className="ws-row-name">{ent.name}</span>
                      <span className="ws-row-meta">
                        {ent.isShared && <span className="ws-badge">{t('sharedBadge')}</span>}
                        {!ent.isDir && <span>{formatFileSize(ent.size) || '0 B'}</span>}
                        {ent.modTime && <span>{formatStamp(ent.modTime, lang)}</span>}
                      </span>
                    </span>
                  </button>
                );
              })}
              {listData.truncated && (
                <div className="ws-empty">{t('filesTruncatedHint')}</div>
              )}
            </>
          )}
        </div>
      </div>

      <div className="ws-preview-col">
        {!selectedPath ? (
          <div className="ws-preview-empty">{t('selectFileHint')}</div>
        ) : (
          <>
            <div className="ws-preview-hd">
              <button
                type="button"
                className="ghost ws-back-btn"
                aria-label={t('stageBackToList')}
                onClick={() => {
                  setSelectedPath('');
                  setPreview(null);
                  setImageUrl('');
                }}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M15 18l-6-6 6-6" />
                </svg>
              </button>
              <div className="ws-preview-title" title={selectedPath}>
                {selectedPath}
              </div>
              <div className="ws-preview-acts">
                <button
                  type="button"
                  className="btn"
                  onClick={() => onQuotePath(selectedPath)}
                >
                  {t('insertToChat')}
                </button>
                <button
                  type="button"
                  className="btn"
                  onClick={() => void handleCopyPath(selectedPath)}
                >
                  {t('copyPath')}
                </button>
                <button
                  type="button"
                  className="btn"
                  onClick={() => void handleDownload(selectedPath, preview?.name || selectedPath.split('/').pop() || 'file')}
                >
                  {t('downloadFile')}
                </button>
              </div>
            </div>

            <div className="ws-preview-body">
              {previewLoading ? (
                <div className="ws-empty">{t('loading')}</div>
              ) : previewErr ? (
                <div className="ws-empty ws-err">{previewErr}</div>
              ) : !preview ? null : preview.kind === 'image' ? (
                <div className="ws-img-wrap">
                  {imageUrl && <img src={imageUrl} alt={preview.name} className="ws-img" />}
                </div>
              ) : preview.kind === 'binary' ? (
                <div className="ws-binary-card">
                  <div className="ws-binary-name">{preview.name}</div>
                  <div className="ws-binary-meta">
                    {preview.mimeType} · {formatFileSize(preview.size) || '0 B'}
                  </div>
                  <p className="ws-binary-hint">{t('binaryFileHint')}</p>
                  <button
                    type="button"
                    className="btn go"
                    onClick={() => void handleDownload(preview.path, preview.name)}
                  >
                    {t('downloadFile')}
                  </button>
                </div>
              ) : (
                <>
                  {preview.truncated && (
                    <div className="note ws-trunc-note">
                      {t('fileTruncatedHint', { size: formatFileSize(preview.content?.length || 0) || '256 KB' })}
                    </div>
                  )}
                  {preview.kind === 'markdown' ? (
                    <div
                      className="md ws-md"
                      dangerouslySetInnerHTML={{
                        __html: renderMarkdownHtml(preview.content || '', t('copy'), {
                          details: t('mdDetails'),
                          image: t('mdImage'),
                        }),
                      }}
                    />
                  ) : (
                    <pre className="ws-code">{preview.content || ''}</pre>
                  )}
                </>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
};
