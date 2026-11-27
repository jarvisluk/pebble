import React, { useCallback, useEffect, useMemo, useState } from 'react';
import type {
  MemoryConfig,
  MemoryMagpieInfo,
  MemoryStatus,
  ResolvedLang,
  SettingsData,
} from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { triggerMemoryDream, triggerMemoryReindex } from '../../lib/memoryApi';
import { MemoryExtrasSection } from './MemoryExtrasSection';

interface MemorySettingsCardProps {
  settings: SettingsData | null;
  memStatus: MemoryStatus | null;
  magpieInfo?: MemoryMagpieInfo;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onRefreshAll: () => Promise<void>;
  onConfigDirtyChange: (dirty: boolean) => void;
  saveConfigRef: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetConfigRef: React.MutableRefObject<(() => void) | null>;
}

export const MemorySettingsCard: React.FC<MemorySettingsCardProps> = ({
  settings,
  memStatus,
  magpieInfo,
  lang,
  onToast,
  onSettingsUpdated,
  onRefreshAll,
  onConfigDirtyChange,
  saveConfigRef,
  resetConfigRef,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const baseCfg = useMemo<MemoryConfig>(
    () => settings?.memory ?? memStatus?.config ?? {},
    [settings?.memory, memStatus?.config]
  );

  const baseCoexistence = baseCfg.coexistence === 'isolated' ? 'isolated' : 'auto';
  const baseDreamEnabled = Boolean(baseCfg.dream?.enabled);
  const baseDreamDailyAt = baseCfg.dream?.dailyAt || '03:00';
  const baseDreamModel = (baseCfg.dream?.model || '').trim();
  const baseDreamMinMsgs =
    typeof baseCfg.dream?.minNewMessages === 'number' && baseCfg.dream.minNewMessages > 0
      ? String(baseCfg.dream.minNewMessages)
      : '';
  const baseEmbedEnabled = Boolean(baseCfg.embedding?.enabled);
  const baseEmbedBaseUrl = (baseCfg.embedding?.baseUrl || '').trim();
  const baseEmbedModel = (baseCfg.embedding?.model || '').trim();
  const hasSavedEmbedKey = Boolean(baseCfg.embedding?.apiKey);
  const baseSnapshotMaxChars =
    typeof baseCfg.snapshotMaxChars === 'number' && baseCfg.snapshotMaxChars > 0
      ? String(baseCfg.snapshotMaxChars)
      : '';
  const baseDisableClaudeAuto = Boolean(baseCfg.disableClaudeAutoMemory);

  const [coexistence, setCoexistence] = useState<'auto' | 'isolated'>(baseCoexistence);
  const [dreamEnabled, setDreamEnabled] = useState(baseDreamEnabled);
  const [dreamDailyAt, setDreamDailyAt] = useState(baseDreamDailyAt);
  const [dreamModel, setDreamModel] = useState(baseDreamModel);
  const [dreamMinMsgs, setDreamMinMsgs] = useState(baseDreamMinMsgs);
  const [embedEnabled, setEmbedEnabled] = useState(baseEmbedEnabled);
  const [embedBaseUrl, setEmbedBaseUrl] = useState(baseEmbedBaseUrl);
  const [embedModel, setEmbedModel] = useState(baseEmbedModel);
  const [embedApiKey, setEmbedApiKey] = useState('');
  const [snapshotMaxChars, setSnapshotMaxChars] = useState(baseSnapshotMaxChars);
  const [disableClaudeAuto, setDisableClaudeAuto] = useState(baseDisableClaudeAuto);
  const [touched, setTouched] = useState(false);

  const [saving, setSaving] = useState(false);
  const [dreamRunning, setDreamRunning] = useState(false);
  const [reindexing, setReindexing] = useState(false);
  const [err, setErr] = useState('');

  const resetDraft = useCallback(() => {
    setCoexistence(baseCoexistence);
    setDreamEnabled(baseDreamEnabled);
    setDreamDailyAt(baseDreamDailyAt);
    setDreamModel(baseDreamModel);
    setDreamMinMsgs(baseDreamMinMsgs);
    setEmbedEnabled(baseEmbedEnabled);
    setEmbedBaseUrl(baseEmbedBaseUrl);
    setEmbedModel(baseEmbedModel);
    setEmbedApiKey('');
    setSnapshotMaxChars(baseSnapshotMaxChars);
    setDisableClaudeAuto(baseDisableClaudeAuto);
    setTouched(false);
    setErr('');
  }, [
    baseCoexistence,
    baseDreamEnabled,
    baseDreamDailyAt,
    baseDreamModel,
    baseDreamMinMsgs,
    baseEmbedEnabled,
    baseEmbedBaseUrl,
    baseEmbedModel,
    baseSnapshotMaxChars,
    baseDisableClaudeAuto,
  ]);

  useEffect(() => {
    if (!touched) {
      setCoexistence(baseCoexistence);
      setDreamEnabled(baseDreamEnabled);
      setDreamDailyAt(baseDreamDailyAt);
      setDreamModel(baseDreamModel);
      setDreamMinMsgs(baseDreamMinMsgs);
      setEmbedEnabled(baseEmbedEnabled);
      setEmbedBaseUrl(baseEmbedBaseUrl);
      setEmbedModel(baseEmbedModel);
      setEmbedApiKey('');
      setSnapshotMaxChars(baseSnapshotMaxChars);
      setDisableClaudeAuto(baseDisableClaudeAuto);
    }
  }, [
    touched,
    baseCoexistence,
    baseDreamEnabled,
    baseDreamDailyAt,
    baseDreamModel,
    baseDreamMinMsgs,
    baseEmbedEnabled,
    baseEmbedBaseUrl,
    baseEmbedModel,
    baseSnapshotMaxChars,
    baseDisableClaudeAuto,
  ]);

  const isDirty = Boolean(
    touched &&
      (coexistence !== baseCoexistence ||
        dreamEnabled !== baseDreamEnabled ||
        dreamDailyAt.trim() !== baseDreamDailyAt ||
        dreamModel.trim() !== baseDreamModel ||
        dreamMinMsgs.trim() !== baseDreamMinMsgs ||
        embedEnabled !== baseEmbedEnabled ||
        embedBaseUrl.trim() !== baseEmbedBaseUrl ||
        embedModel.trim() !== baseEmbedModel ||
        embedApiKey.trim() !== '' ||
        snapshotMaxChars.trim() !== baseSnapshotMaxChars ||
        disableClaudeAuto !== baseDisableClaudeAuto)
  );

  useEffect(() => {
    onConfigDirtyChange(isDirty);
  }, [isDirty, onConfigDirtyChange]);

  const saveConfig = useCallback(async (): Promise<boolean> => {
    setSaving(true);
    setErr('');
    try {
      const parsedMin = parseInt(dreamMinMsgs.trim(), 10);
      const parsedSnap = parseInt(snapshotMaxChars.trim(), 10);
      const memoryPayload: MemoryConfig = {
        enabled: baseCfg.enabled ?? true,
        coexistence,
        disableClaudeAutoMemory: disableClaudeAuto,
        ...(Number.isFinite(parsedSnap) && parsedSnap > 0 ? { snapshotMaxChars: parsedSnap } : {}),
        dream: {
          enabled: dreamEnabled,
          dailyAt: dreamDailyAt.trim() || undefined,
          model: dreamModel.trim() || undefined,
          ...(Number.isFinite(parsedMin) && parsedMin > 0 ? { minNewMessages: parsedMin } : {}),
        },
        embedding: {
          enabled: embedEnabled,
          baseUrl: embedBaseUrl.trim() || undefined,
          model: embedModel.trim() || undefined,
          ...(embedApiKey.trim() ? { apiKey: embedApiKey.trim() } : {}),
        },
      };
      const next = await api<SettingsData>('/api/settings', {
        method: 'PUT',
        body: { memory: memoryPayload },
      });
      onSettingsUpdated(next);
      setEmbedApiKey('');
      setTouched(false);
      onToast(translate(lang, 'memConfigSaved'));
      await onRefreshAll();
      return true;
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      return false;
    } finally {
      setSaving(false);
    }
  }, [
    baseCfg.enabled,
    coexistence,
    disableClaudeAuto,
    dreamDailyAt,
    dreamEnabled,
    dreamMinMsgs,
    dreamModel,
    embedApiKey,
    embedBaseUrl,
    embedEnabled,
    embedModel,
    lang,
    onRefreshAll,
    onSettingsUpdated,
    onToast,
    snapshotMaxChars,
  ]);

  useEffect(() => {
    saveConfigRef.current = saveConfig;
    resetConfigRef.current = resetDraft;
    return () => {
      saveConfigRef.current = null;
      resetConfigRef.current = null;
    };
  }, [saveConfigRef, resetConfigRef, saveConfig, resetDraft]);

  const handleRunDream = async () => {
    setDreamRunning(true);
    setErr('');
    try {
      await triggerMemoryDream();
      onToast(t('memDreamDone'));
      await onRefreshAll();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setDreamRunning(false);
    }
  };

  const handleReindex = async () => {
    setReindexing(true);
    setErr('');
    try {
      const res = await triggerMemoryReindex();
      onToast(
        t('memReindexDone', {
          docs: res.docsIndexed ?? 0,
          chunks: res.chunksIndexed ?? 0,
        })
      );
      await onRefreshAll();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setReindexing(false);
    }
  };

  const effectiveMagpie = memStatus?.magpie ?? magpieInfo;
  const recentDreams = memStatus?.recentDreams || [];

  return (
    <section className="set-card" id="sMemSettingsCard">
      <div className="set-card-hd">
        <div>
          <h2>{t('memSettingsTitle')}</h2>
          <span className="set-card-desc">{t('memSettingsDesc')}</span>
        </div>
        <div className="mem-card-hd-acts">
          {typeof memStatus?.docCount === 'number' && (
            <span className="mp">
              {t('memIndexStats', {
                docs: memStatus.docCount,
                chunks: memStatus.chunkCount ?? 0,
              })}
            </span>
          )}
          <button
            type="button"
            id="sMemCfgSave"
            className="mp go"
            disabled={!isDirty || saving}
            onClick={() => void saveConfig()}
          >
            {saving ? t('saving') : t('save')}
          </button>
        </div>
      </div>

      <div className="row-inline-sm row-wrap">
        <button
          type="button"
          id="sMemRunDream"
          className="mp"
          disabled={dreamRunning}
          onClick={() => void handleRunDream()}
        >
          {dreamRunning ? t('memRunningDream') : t('memRunDreamBtn')}
        </button>
        <button
          type="button"
          id="sMemReindex"
          className="mp"
          disabled={reindexing}
          onClick={() => void handleReindex()}
        >
          {reindexing ? t('memReindexing') : t('memReindexBtn')}
        </button>
      </div>

      <div className="fld">
        <label htmlFor="sMemCoexistence">{t('memCoexistenceLbl')}</label>
        <select
          id="sMemCoexistence"
          value={coexistence}
          onChange={(e) => {
            setCoexistence(e.target.value === 'isolated' ? 'isolated' : 'auto');
            setTouched(true);
          }}
        >
          <option value="auto">{t('memCoexistenceAuto')}</option>
          <option value="isolated">{t('memCoexistenceIsolated')}</option>
        </select>
        <div className="hint mt-4">{t('memCoexistenceHint')}</div>
      </div>

      <div className="mem-cfg-sec">
        <div className="mem-toggle-row">
          <label htmlFor="sMemDreamEnable">{t('memDreamEnable')}</label>
          <button
            type="button"
            id="sMemDreamEnable"
            role="switch"
            aria-checked={dreamEnabled}
            className={`sw${dreamEnabled ? ' on' : ''}`}
            onClick={() => {
              setDreamEnabled((v) => !v);
              setTouched(true);
            }}
          />
        </div>
        {dreamEnabled && (
          <div className="two">
            <div className="fld">
              <label
                htmlFor="sMemDreamDailyAt"
                dangerouslySetInnerHTML={{ __html: t('memDreamDailyAt') }}
              />
              <input
                id="sMemDreamDailyAt"
                className="mono"
                placeholder="03:00"
                value={dreamDailyAt}
                onChange={(e) => {
                  setDreamDailyAt(e.target.value);
                  setTouched(true);
                }}
              />
            </div>
            <div className="fld">
              <label
                htmlFor="sMemDreamModel"
                dangerouslySetInnerHTML={{ __html: t('memDreamModel') }}
              />
              <input
                id="sMemDreamModel"
                className="mono"
                list="models-global"
                spellCheck={false}
                placeholder={t('memDreamModelPh')}
                value={dreamModel}
                onChange={(e) => {
                  setDreamModel(e.target.value);
                  setTouched(true);
                }}
              />
            </div>
          </div>
        )}
      </div>

      <div className="mem-cfg-sec">
        <div className="mem-toggle-row">
          <label htmlFor="sMemEmbedEnable">{t('memEmbedEnable')}</label>
          <button
            type="button"
            id="sMemEmbedEnable"
            role="switch"
            aria-checked={embedEnabled}
            className={`sw${embedEnabled ? ' on' : ''}`}
            onClick={() => {
              setEmbedEnabled((v) => !v);
              setTouched(true);
            }}
          />
        </div>
        {embedEnabled && (
          <div className="set-stack">
            <div className="two">
              <div className="fld">
                <label htmlFor="sMemEmbedBaseUrl">{t('memEmbedBaseUrl')}</label>
                <input
                  id="sMemEmbedBaseUrl"
                  className="mono"
                  spellCheck={false}
                  placeholder={t('memEmbedBaseUrlPh')}
                  value={embedBaseUrl}
                  onChange={(e) => {
                    setEmbedBaseUrl(e.target.value);
                    setTouched(true);
                  }}
                />
              </div>
              <div className="fld">
                <label htmlFor="sMemEmbedModel">{t('memEmbedModel')}</label>
                <input
                  id="sMemEmbedModel"
                  className="mono"
                  spellCheck={false}
                  placeholder={t('memEmbedModelPh')}
                  value={embedModel}
                  onChange={(e) => {
                    setEmbedModel(e.target.value);
                    setTouched(true);
                  }}
                />
              </div>
            </div>
            <div className="fld">
              <label htmlFor="sMemEmbedApiKey">{t('memEmbedApiKey')}</label>
              <input
                id="sMemEmbedApiKey"
                type="password"
                className="mono"
                autoComplete="off"
                placeholder={hasSavedEmbedKey ? t('memEmbedApiKeySetPh') : t('memEmbedApiKeyPh')}
                value={embedApiKey}
                onChange={(e) => {
                  setEmbedApiKey(e.target.value);
                  setTouched(true);
                }}
              />
            </div>
          </div>
        )}
      </div>

      <details className="ts-adv">
        <summary className="link-btn">{t('memAdvSection')}</summary>
        <div className="ts-adv-body">
          <div className="two">
            <div className="fld">
              <label htmlFor="sMemSnapshotMax">{t('memSnapshotMaxChars')}</label>
              <input
                id="sMemSnapshotMax"
                type="number"
                min={500}
                className="mono"
                placeholder="12000"
                value={snapshotMaxChars}
                onChange={(e) => {
                  setSnapshotMaxChars(e.target.value);
                  setTouched(true);
                }}
              />
            </div>
            <div className="fld">
              <label htmlFor="sMemDreamMinMsgs">{t('memDreamMinMsgs')}</label>
              <input
                id="sMemDreamMinMsgs"
                type="number"
                min={1}
                className="mono"
                placeholder="6"
                value={dreamMinMsgs}
                onChange={(e) => {
                  setDreamMinMsgs(e.target.value);
                  setTouched(true);
                }}
              />
            </div>
          </div>
          <div className="mem-toggle-row">
            <label htmlFor="sMemDisableClaude">{t('memDisableClaudeAuto')}</label>
            <button
              type="button"
              id="sMemDisableClaude"
              role="switch"
              aria-checked={disableClaudeAuto}
              className={`sw${disableClaudeAuto ? ' on' : ''}`}
              onClick={() => {
                setDisableClaudeAuto((v) => !v);
                setTouched(true);
              }}
            />
          </div>
        </div>
      </details>

      <MemoryExtrasSection
        magpie={effectiveMagpie}
        recentDreams={recentDreams}
        lang={lang}
        onToast={onToast}
        onError={setErr}
        onRefreshAll={onRefreshAll}
      />

      {err && <div className="msg-err">{err}</div>}
    </section>
  );
};
