import React, { useCallback, useEffect, useRef, useState } from 'react';
import type { ResolvedLang, WorkspaceFileContent } from '../../types';
import {
  fetchWorkspaceFileContent,
  requestWorkspaceFileTicket,
  workspaceFileRawURL,
} from '../../lib/api';
import { translate } from '../../lib/i18n';
import { renderMarkdownHtml } from '../../lib/markdown';
import { formatFileSize } from '../ChatView';

interface StageFilePreviewPaneProps {
  botId: string;
  filePath: string;
  lang: ResolvedLang;
  onQuotePath: (relPath: string) => void;
  onToast: (msg: string) => void;
}

export const StageFilePreviewPane: React.FC<StageFilePreviewPaneProps> = ({
  botId,
  filePath,
  lang,
  onQuotePath,
  onToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [preview, setPreview] = useState<WorkspaceFileContent | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [imageUrl, setImageUrl] = useState('');
  const seqRef = useRef(0);

  const loadFile = useCallback(async () => {
    if (!botId || !filePath) return;
    const seq = ++seqRef.current;
    setLoading(true);
    setErr('');
    setImageUrl('');
    try {
      const doc = await fetchWorkspaceFileContent(botId, filePath);
      if (seq !== seqRef.current) return;
      setPreview(doc);
      if (doc.kind === 'image') {
        const tk = await requestWorkspaceFileTicket(botId, filePath);
        if (seq !== seqRef.current) return;
        setImageUrl(workspaceFileRawURL(botId, filePath, tk.ticket, false));
      }
    } catch (e) {
      if (seq !== seqRef.current) return;
      setPreview(null);
      setErr(e instanceof Error ? e.message : t('opFailed'));
    } finally {
      if (seq === seqRef.current) setLoading(false);
    }
  }, [botId, filePath]);

  useEffect(() => {
    setPreview(null);
    setErr('');
    setImageUrl('');
    void loadFile();
  }, [loadFile]);

  const handleDownload = async () => {
    try {
      const fileName = preview?.name || filePath.split('/').pop() || 'file';
      const tk = await requestWorkspaceFileTicket(botId, filePath);
      const url = workspaceFileRawURL(botId, filePath, tk.ticket, true);
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

  const handleCopyPath = async () => {
    try {
      await navigator.clipboard.writeText(filePath);
      onToast(t('pathCopied'));
    } catch {
      onToast(filePath);
    }
  };

  return (
    <div className="stage-file-wrap">
      <div className="ws-preview-col stage-file-card">
        <div className="ws-preview-hd">
          <div className="ws-preview-title" title={filePath}>
            {filePath}
          </div>
          <div className="ws-preview-acts">
            <button type="button" className="btn" onClick={() => onQuotePath(filePath)}>
              {t('insertToChat')}
            </button>
            <button type="button" className="btn" onClick={() => void handleCopyPath()}>
              {t('copyPath')}
            </button>
            <button type="button" className="btn" onClick={() => void handleDownload()}>
              {t('downloadFile')}
            </button>
            <button
              type="button"
              className="ghost ws-icon-btn"
              aria-label={t('refreshFiles')}
              title={t('refreshFiles')}
              onClick={() => void loadFile()}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 12a9 9 0 1 1-2.64-6.36" />
                <path d="M21 3v6h-6" />
              </svg>
            </button>
          </div>
        </div>

        <div className="ws-preview-body">
          {loading ? (
            <div className="ws-empty">{t('loading')}</div>
          ) : err ? (
            <div className="ws-empty ws-err">
              <div>{err}</div>
              <button type="button" className="btn ws-retry-btn" onClick={() => void loadFile()}>
                {t('retry')}
              </button>
            </div>
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
              <button type="button" className="btn go" onClick={() => void handleDownload()}>
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
      </div>
    </div>
  );
};
