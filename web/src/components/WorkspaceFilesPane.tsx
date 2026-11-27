import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  ResolvedLang,
  WorkspaceFileEntry,
  WorkspaceListResult,
} from '../types';
import { fetchWorkspaceFiles } from '../lib/api';
import { translate } from '../lib/i18n';
import { formatStamp } from '../lib/theme';
import { formatFileSize } from './ChatView';

interface WorkspaceFilesPaneProps {
  botId: string;
  initialDir?: string;
  openSeq?: number;
  selectedFilePath?: string;
  lang: ResolvedLang;
  onOpenFileTab: (relPath: string) => void;
}

export const WorkspaceFilesPane: React.FC<WorkspaceFilesPaneProps> = ({
  botId,
  initialDir = '',
  openSeq = 0,
  selectedFilePath = '',
  lang,
  onOpenFileTab,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [dirPath, setDirPath] = useState('');
  const [showHidden, setShowHidden] = useState(false);
  const [listData, setListData] = useState<WorkspaceListResult | null>(null);
  const [listLoading, setListLoading] = useState(false);
  const [listErr, setListErr] = useState('');
  const listSeqRef = useRef(0);

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

  useEffect(() => {
    if (!botId) return;
    void loadDir(initialDir, showHidden);
  }, [botId, initialDir, openSeq]);

  const handleToggleHidden = () => {
    const next = !showHidden;
    setShowHidden(next);
    void loadDir(dirPath, next);
  };

  const handleEntryClick = (ent: WorkspaceFileEntry) => {
    if (ent.isDir) {
      void loadDir(ent.path, showHidden);
    } else {
      onOpenFileTab(ent.path);
    }
  };

  const segments = dirPath ? dirPath.split('/').filter(Boolean) : [];

  return (
    <div className="ws-browser-wrap">
      <div className="ws-tree-col ws-browser-card">
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
                const active = !ent.isDir && ent.path === selectedFilePath;
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
                    {!ent.isDir && (
                      <span className="ws-row-chev">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="m9 18 6-6-6-6" />
                        </svg>
                      </span>
                    )}
                  </button>
                );
              })}
              {listData.truncated && <div className="ws-empty">{t('filesTruncatedHint')}</div>}
            </>
          )}
        </div>
      </div>
    </div>
  );
};
