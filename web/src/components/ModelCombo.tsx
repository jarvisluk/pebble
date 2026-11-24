import React, { useEffect, useMemo, useRef, useState } from 'react';

interface ModelComboProps {
  id: string;
  value: string;
  onChange: (next: string) => void;
  options: string[];
  loading?: boolean;
  placeholder: string;
  loadingText: string;
  emptyText: string;
  dropUp?: boolean;
}

export const ModelCombo: React.FC<ModelComboProps> = ({
  id,
  value,
  onChange,
  options,
  loading = false,
  placeholder,
  loadingText,
  emptyText,
  dropUp = false,
}) => {
  const [open, setOpen] = useState(false);
  const [filtering, setFiltering] = useState(false);
  const [activeIdx, setActiveIdx] = useState(0);
  const wrapRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDocDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
        setFiltering(false);
      }
    };
    document.addEventListener('mousedown', onDocDown);
    return () => document.removeEventListener('mousedown', onDocDown);
  }, [open]);

  const filtered = useMemo(() => {
    if (!filtering || !value.trim()) return options;
    const q = value.trim().toLowerCase();
    return options.filter((m) => m.toLowerCase().includes(q));
  }, [options, value, filtering]);

  useEffect(() => {
    setActiveIdx(0);
  }, [filtered.length, open]);

  const pick = (m: string) => {
    onChange(m);
    setOpen(false);
    setFiltering(false);
  };

  return (
    <div ref={wrapRef} className={`mcombo${open ? ' open' : ''}`}>
      <div className="mcombo-input-wrap">
        <input
          id={id}
          className="mono mcombo-input"
          spellCheck={false}
          autoComplete="off"
          placeholder={placeholder}
          value={value}
          onFocus={() => {
            setOpen(true);
            setFiltering(false);
          }}
          onClick={() => {
            setOpen(true);
          }}
          onChange={(e) => {
            onChange(e.target.value);
            setFiltering(true);
            setOpen(true);
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault();
              if (!open) {
                setOpen(true);
              } else if (filtered.length > 0) {
                setActiveIdx((i) => (i + 1) % filtered.length);
              }
            } else if (e.key === 'ArrowUp') {
              e.preventDefault();
              if (filtered.length > 0) {
                setActiveIdx((i) => (i - 1 + filtered.length) % filtered.length);
              }
            } else if (e.key === 'Enter' && open && filtered[activeIdx]) {
              e.preventDefault();
              pick(filtered[activeIdx]);
            } else if (e.key === 'Escape' && open) {
              e.preventDefault();
              setOpen(false);
              setFiltering(false);
            }
          }}
        />
        {value && (
          <button
            type="button"
            className="mcombo-clear"
            aria-label="Clear"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              onChange('');
              setFiltering(false);
              setOpen(true);
            }}
          >
            ×
          </button>
        )}
        <button
          type="button"
          className="mcombo-toggle"
          aria-label="Toggle models"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            setOpen((v) => !v);
            setFiltering(false);
          }}
        >
          <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <path
              d="M4.5 6.5L8 10l3.5-3.5"
              stroke="currentColor"
              strokeWidth="1.6"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </button>
      </div>

      {open && (
        <div className={`mcombo-pop${dropUp ? ' up' : ''}`} role="listbox">
          {loading && filtered.length === 0 ? (
            <div className="mcombo-empty">{loadingText}</div>
          ) : filtered.length === 0 ? (
            <div className="mcombo-empty">{emptyText}</div>
          ) : (
            filtered.map((m, idx) => (
              <button
                key={m}
                type="button"
                role="option"
                aria-selected={m === value}
                className={`mcombo-opt mono${m === value ? ' sel' : ''}${idx === activeIdx ? ' hi' : ''}`}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setActiveIdx(idx)}
                onClick={() => pick(m)}
              >
                {m}
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
};
