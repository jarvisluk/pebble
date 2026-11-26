import React, { useEffect, useState } from 'react';
import type { MemoryResponse, ResolvedLang, SettingsData } from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { formatStamp } from '../../lib/theme';
import { MagpieInstallBanner } from '../MagpieInstallBanner';

interface BotMemorySectionProps {
  open: boolean;
  botId: string;
  settings: SettingsData | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated?: (s: SettingsData) => void;
}

export const BotMemorySection: React.FC<BotMemorySectionProps> = ({
  open,
  botId,
  settings,
  lang,
  onToast,
  onSettingsUpdated,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memErr, setMemErr] = useState('');
  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memKind, setMemKind] = useState('fact');
  const [memText, setMemText] = useState('');
  const [memSaving, setMemSaving] = useState(false);

  const loadMemories = async (targetBotId: string) => {
    setMemLoading(true);
    setMemErr('');
    try {
      const d = await api<MemoryResponse>(`/api/memory?botId=${encodeURIComponent(targetBotId)}&limit=30`);
      setMemData(d);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setMemLoading(false);
    }
  };

  useEffect(() => {
    if (!open || !botId) return;
    setMemAddOpen(false);
    setMemErr('');
    void loadMemories(botId);
  }, [open, botId]);

  const handleSaveMemory = async () => {
    if (!botId) return;
    const text = memText.trim();
    if (!text) {
      setMemErr(t('memContentReq'));
      return;
    }
    setMemSaving(true);
    setMemErr('');
    try {
      await api('/api/memory', {
        method: 'POST',
        body: { botId: botId, kind: memKind, text },
      });
      setMemText('');
      setMemAddOpen(false);
      onToast(t('botMemSaved'));
      await loadMemories(botId);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setMemSaving(false);
    }
  };

  const handleDeleteMemory = async (memId: string) => {
    if (!botId) return;
    setMemErr('');
    try {
      await api(`/api/memory/${encodeURIComponent(memId)}`, { method: 'DELETE' });
      onToast(t('memDeleted'));
      await loadMemories(botId);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    }
  };


  return (
    <>
    <div className="sec row-between">
      <span>{t('botMemory')}</span>
      <button
        type="button"
        id="pMemAddToggle"
        className="mp go"
        onClick={() => setMemAddOpen((v) => !v)}
      >
        {t('addShort')}
      </button>
    </div>
    {memAddOpen && (
      <div id="pMemAddBox" className="add-prov mem-add mb-8">
        <div className="fld">
          <label>{t('memType')}</label>
          <select id="pMemKind" value={memKind} onChange={(e) => setMemKind(e.target.value)}>
            <option value="fact">{t('kindFact')}</option>
            <option value="rule">{t('kindRule')}</option>
            <option value="reference">{t('kindReference')}</option>
            <option value="procedure">{t('kindProcedure')}</option>
            <option value="episode">{t('kindEpisode')}</option>
          </select>
        </div>
        <div className="fld">
          <label>{t('memContent')}</label>
          <textarea
            id="pMemText"
            rows={3}
            className="ta-sm ta-font"
            placeholder={t('botMemPh')}
            value={memText}
            onChange={(e) => setMemText(e.target.value)}
          />
        </div>
        <div className="row-end-sm">
          <button
            type="button"
            id="pMemAddCancel"
            className="mp"
            onClick={() => {
              setMemAddOpen(false);
              setMemErr('');
            }}
          >
            {t('cancel')}
          </button>
          <button
            type="button"
            id="pMemAddSubmit"
            className="mp go"
            disabled={memSaving}
            onClick={() => void handleSaveMemory()}
          >
            {t('save')}
          </button>
        </div>
      </div>
    )}

    <div id="pMemList" className="stack6">
      {memLoading && !memData ? (
        <div className="set-card-desc">{t('loadingEllipsis')}</div>
      ) : !memData || !memData.installed ? (
        <div className="note">
          <MagpieInstallBanner
            lang={lang}
            hint={t('memNotInstalled')}
            installCmds={settings?.magpie?.install}
            onToast={onToast}
            onInstalled={async () => {
              if (onSettingsUpdated) {
                const fresh = await api<SettingsData>('/api/settings?fresh=1');
                onSettingsUpdated(fresh);
              }
              await loadMemories(botId);
            }}
          />
        </div>
      ) : !memData.available ? (
        <div className="set-card-desc">
          {t('memUnavailable')}
          {memData.error ? `: ${memData.error}` : ''}
        </div>
      ) : !(memData.items || []).length ? (
        <div className="set-card-desc">{t('memEmpty')}</div>
      ) : (
        (memData.items || []).map((it) => {
          const tags = (it.tags || []).filter((tag) => !tag.startsWith('bot:'));
          const tm = it.updatedAt ? it.updatedAt.replace('T', ' ').slice(0, 16) : '';
          return (
            <div key={it.id} className="mem-row">
              <div className="mem-top">
                <div className="mem-meta">
                  {it.kind && <span className="mp ok">{it.kind}</span>}
                  {tags.map((tg) => (
                    <span key={tg} className="mp">
                      #{tg}
                    </span>
                  ))}
                  {tm && <span className="mem-time">{tm}</span>}
                </div>
                <button type="button" className="mp bad" onClick={() => void handleDeleteMemory(it.id)}>
                  {t('delBtn')}
                </button>
              </div>
              <div className="mem-txt">{it.text}</div>
            </div>
          );
        })
      )}
    </div>
    <div id="pMemErr" className="msg-err">
      {memErr}
    </div>
    </>
  );
};
