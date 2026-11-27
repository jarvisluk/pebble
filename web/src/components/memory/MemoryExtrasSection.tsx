import React, { useState } from 'react';
import type { MemoryDreamEntry, MemoryMagpieInfo, ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import { formatMemoryStamp, importFromMagpie } from '../../lib/memoryApi';

interface MemoryExtrasSectionProps {
  magpie?: MemoryMagpieInfo;
  recentDreams: MemoryDreamEntry[];
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onError: (msg: string) => void;
  onRefreshAll: () => Promise<void>;
}

export const MemoryExtrasSection: React.FC<MemoryExtrasSectionProps> = ({
  magpie,
  recentDreams,
  lang,
  onToast,
  onError,
  onRefreshAll,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [importMode, setImportMode] = useState<'bots_only' | 'all'>('bots_only');
  const [importing, setImporting] = useState(false);

  const showMagpieImport = Boolean(magpie?.installed || magpie?.hasLegacyItems);

  const handleImportMagpie = async () => {
    setImporting(true);
    onError('');
    try {
      const res = await importFromMagpie({ mode: importMode });
      onToast(
        t('memImportDone', {
          imported: res.imported ?? 0,
          skipped: res.skipped ?? 0,
        })
      );
      await onRefreshAll();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    } finally {
      setImporting(false);
    }
  };

  return (
    <>
      {showMagpieImport && (
        <div className="note" id="sMemMagpieImport">
          <div className="row-between row-wrap">
            <strong>{t('memMagpieImportTitle')}</strong>
            {(magpie?.legacyCount ?? 0) > 0 && (
              <span className="mp">
                {t('memMagpieLegacyCount', { n: magpie?.legacyCount ?? 0 })}
              </span>
            )}
          </div>
          <div>{t('memMagpieImportDesc')}</div>
          {(magpie?.injectedHarnesses || []).length > 0 && (
            <div className="mem-sub-meta mem-wrap-line">
              {t('memMagpieInjected', {
                list: (magpie?.injectedHarnesses || []).join(', '),
              })}
            </div>
          )}
          <div className="mem-import-row">
            <select
              id="sMemImportMode"
              aria-label={t('memMagpieImportTitle')}
              value={importMode}
              onChange={(e) =>
                setImportMode(e.target.value === 'all' ? 'all' : 'bots_only')
              }
            >
              <option value="bots_only">{t('memImportModeBots')}</option>
              <option value="all">{t('memImportModeAll')}</option>
            </select>
            <button
              type="button"
              id="sMemImportBtn"
              className="mp go"
              disabled={importing}
              onClick={() => void handleImportMagpie()}
            >
              {importing ? t('memImporting') : t('memImportBtn')}
            </button>
          </div>
        </div>
      )}

      {recentDreams.length > 0 && (
        <div className="mem-cfg-sec">
          <div className="mem-cfg-sec-hd">
            <h3>{t('memRecentDreamsTitle')}</h3>
          </div>
          <div className="mem-dream-list">
            {recentDreams.map((entry, idx) => (
              <div
                key={`${entry.botId || 'global'}-${entry.date}-${idx}`}
                className="mem-dream-item"
              >
                <div className="mem-dream-hd">
                  <span>
                    {entry.botId ? `@${entry.botId}` : t('memRecentDreamsGlobal')} · {entry.date}
                  </span>
                  {entry.updatedAt && <span>{formatMemoryStamp(entry.updatedAt)}</span>}
                </div>
                {entry.summary && <div className="mem-dream-sum">{entry.summary}</div>}
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
};
