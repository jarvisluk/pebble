import React from 'react';
import type { MessageMedia, ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';

export function isImageMedia(m: MessageMedia): boolean {
  if (m.kind === 'image') return true;
  if (m.kind === 'file') return false;
  return Boolean(m.mimeType && m.mimeType.startsWith('image/'));
}

export function formatFileSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export const MessageAttachments: React.FC<{
  media?: MessageMedia[];
  lang: ResolvedLang;
  onPreviewImage: (img: MessageMedia) => void;
}> = ({ media, lang, onPreviewImage }) => {
  if (!media || media.length === 0) return null;
  const t = (k: string) => translate(lang, k);
  const images = media.filter(isImageMedia);
  const files = media.filter((m) => !isImageMedia(m));

  return (
    <>
      {images.length > 0 && (
        <div className={`msg-media${images.length === 1 ? ' single' : ''}`}>
          {images.map((img) => (
            <button
              key={img.id}
              type="button"
              className="msg-img-btn"
              onClick={() => onPreviewImage(img)}
            >
              <img
                className="msg-img"
                src={img.url}
                alt={img.alt || img.name || t('imageAltFallback')}
                loading="lazy"
              />
            </button>
          ))}
        </div>
      )}
      {files.length > 0 && (
        <div className="msg-files">
          {files.map((f) => {
            const name = f.name || f.id || t('fileAltFallback');
            const sizeStr = formatFileSize(f.size);
            const href = `${f.url}?download=1&name=${encodeURIComponent(name)}`;
            return (
              <a
                key={f.id}
                className="msg-file-card"
                href={href}
                download={name}
                title={t('downloadFile')}
              >
                <svg
                  className="msg-file-ico"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.9"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                  <path d="M14 3v5h5" />
                </svg>
                <span className="msg-file-meta">
                  <span className="msg-file-name">{name}</span>
                  {sizeStr ? <span className="msg-file-size">{sizeStr}</span> : null}
                </span>
                <svg
                  className="msg-file-dl"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M12 4v12m0 0-4-4m4 4 4-4M4 20h16" />
                </svg>
              </a>
            );
          })}
        </div>
      )}
    </>
  );
};
