import React from 'react';

interface HarnessIconProps {
  id: string;
}

export const HarnessIcon: React.FC<HarnessIconProps> = ({ id }) => {
  const glyph = (() => {
    switch (id) {
      case 'opencode':
        // OpenCode official nested square mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M4 4h16v16H4V4Zm4 4v8h8V8H8Z"
              fill="currentColor"
            />
            <rect x="10.5" y="10.5" width="3" height="3" fill="currentColor" opacity="0.45" />
          </svg>
        );
      case 'claude':
        // Anthropic Claude official 12-ray spark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
            <path
              d="M12 2.5v5.1m0 8.8v5.1M2.5 12h5.1m8.8 0h5.1M5.28 5.28l3.6 3.6m6.24 6.24 3.6 3.6M18.72 5.28l-3.6 3.6m-6.24 6.24-3.6 3.6"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        );
      case 'codex':
        // OpenAI official 6-petal blossom knot
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinejoin="round">
            <path d="M12 3.2a4.1 4.1 0 0 1 3.55 2.05l4.05 7a4.1 4.1 0 0 1-1.5 5.6l-4.05 2.34a4.1 4.1 0 0 1-4.1 0l-4.05-2.34a4.1 4.1 0 0 1-1.5-5.6l4.05-7A4.1 4.1 0 0 1 12 3.2Z" />
            <circle cx="12" cy="12" r="3.2" />
            <path d="M12 3.2v5.6M19.6 7.6l-4.85 2.8M19.6 16.4l-4.85-2.8M12 20.8v-5.6M4.4 16.4l4.85-2.8M4.4 7.6l4.85 2.8" />
          </svg>
        );
      case 'grok':
        // xAI Grok official slashed ring mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
            <path
              d="M18.36 5.64a9 9 0 1 0 0 12.72"
              stroke="currentColor"
              strokeWidth="2.1"
              strokeLinecap="round"
            />
            <path
              d="M6.5 18.5 19.5 4.5"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
            />
          </svg>
        );
      case 'dsh':
        // DeepSeek official whale silhouette mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M19.8 11.4c-.6-2.9-3.1-5.1-6.5-5.4-2.1-.2-3.9.6-5.2 2-.8-.8-2-1.3-3.4-1.2-.3 0-.5.3-.3.6.7.9 1.1 2 .9 3.2-.6 1-.9 2.1-.9 3.3 0 3.7 3.2 6.1 7.8 6.1 4.5 0 7.9-2.8 7.9-6.6 0-.7-.1-1.4-.3-2Zm-9.6 1.2a1.1 1.1 0 1 1 0-2.2 1.1 1.1 0 0 1 0 2.2Z" />
          </svg>
        );
      case 'codebuddy':
        // Tencent CodeBuddy official paired-spark / bracket mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <path d="M8.5 6.5 4 12l4.5 5.5" />
            <path d="M15.5 6.5 20 12l-4.5 5.5" />
            <path d="M12 8.2 13.1 11l2.7 1-2.7 1L12 15.8l-1.1-2.8L8.2 12l2.7-1L12 8.2Z" fill="currentColor" stroke="none" />
          </svg>
        );
      case 'qoder':
        // Alibaba Qoder official geometric Q mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 4.5a7.5 7.5 0 1 0 5.3 12.8" />
            <path d="M19.5 12a7.5 7.5 0 0 0-7.5-7.5" />
            <path d="M13.5 13.5 19.5 19.5" />
          </svg>
        );
      case 'trae':
        // ByteDance Trae official stepped prism mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M5 5h10v4H9v6H5V5Zm6 6h8v8h-8v-8Zm2.5 2.5v3h3v-3h-3Z" />
          </svg>
        );
      case 'pi':
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 7h14M9.5 7v11M15 7v8.5a2.5 2.5 0 0 0 3.2 2.4" />
          </svg>
        );
      default:
        // Antigravity official orbit delta mark
        return (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 3.8 19 18.5l-7-3.2-7 3.2L12 3.8Z" />
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
