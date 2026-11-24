import React from 'react';

interface HarnessIconProps {
  id: string;
}

export const HarnessIcon: React.FC<HarnessIconProps> = ({ id }) => {
  const glyph = (() => {
    switch (id) {
      case 'opencode':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="8 7 3 12 8 17" />
            <polyline points="16 7 21 12 16 17" />
            <line x1="13.5" y1="5" x2="10.5" y2="19" />
          </svg>
        );
      case 'codebuddy':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 6.5A2.5 2.5 0 0 1 7.5 4h9A2.5 2.5 0 0 1 19 6.5v7a2.5 2.5 0 0 1-2.5 2.5H10l-4 3.5V16H7.5A2.5 2.5 0 0 1 5 13.5v-7Z" />
            <circle cx="10" cy="10" r="1" fill="currentColor" stroke="none" />
            <circle cx="14" cy="10" r="1" fill="currentColor" stroke="none" />
          </svg>
        );
      case 'qoder':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11.5" cy="11.5" r="6.5" />
            <line x1="15" y1="15" x2="20" y2="20" />
          </svg>
        );
      case 'codex':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <rect x="4" y="5" width="16" height="14" rx="3" />
            <polyline points="8 10 11 12.5 8 15" />
            <line x1="13" y1="15" x2="16" y2="15" />
          </svg>
        );
      case 'claude':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round">
            <line x1="12" y1="3.5" x2="12" y2="20.5" />
            <line x1="4.6" y1="7.8" x2="19.4" y2="16.2" />
            <line x1="19.4" y1="7.8" x2="4.6" y2="16.2" />
          </svg>
        );
      case 'grok':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="8" />
            <line x1="7.5" y1="16.5" x2="17.5" y2="6.5" />
          </svg>
        );
      case 'trae':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <polygon points="12 4 20 8.5 20 15.5 12 20 4 15.5 4 8.5" />
            <line x1="12" y1="12" x2="12" y2="20" />
            <line x1="12" y1="12" x2="20" y2="8.5" />
            <line x1="12" y1="12" x2="4" y2="8.5" />
          </svg>
        );
      case 'pi':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <line x1="5" y1="7" x2="19" y2="7" />
            <line x1="9" y1="7" x2="9" y2="18" />
            <path d="M15 7v8.5a2.5 2.5 0 0 0 3 2.4" />
          </svg>
        );
      case 'dsh':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 3.5c4.5 3.5 7 7 7 10.5a7 7 0 1 1-14 0c0-3.5 2.5-7 7-10.5Z" />
          </svg>
        );
      default:
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <polygon points="12 3 14.5 9.5 21 12 14.5 14.5 12 21 9.5 14.5 3 12 9.5 9.5" />
          </svg>
        );
    }
  })();

  return (
    <span className="harness-icon" aria-hidden="true">
      {glyph}
    </span>
  );
};
