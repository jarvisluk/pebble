import React, { useEffect, useState } from 'react';
import type {
  BotDetail,
  BotStatus,
  ConfirmOptions,
  MemoryResponse,
  ResolvedLang,
  RoutineItem,
  RoutineRun,
  SettingsData,
} from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';
import { botColor, contrastText, formatStamp } from '../lib/theme';
import { BotAppearancePicker } from './BotAppearancePicker';
import { BotAvatar } from './BotAvatar';
import { MagpieInstallBanner } from './MagpieInstallBanner';

interface BotDetailPanelProps {
  open: boolean;
  detail: BotDetail | null;
  bots: BotStatus[];
  settings: SettingsData | null;
  models: Record<string, string[]>;
  routineRefreshSeq: number;
  lang: ResolvedLang;
  onClose: () => void;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onBotUpdated: (st: BotStatus) => void;
  onBotDeleted: (id: string) => void;
  onReloadBots: () => Promise<void>;
  onLoadModels: (harness: string, force?: boolean) => Promise<unknown>;
  onRefreshDetail: (id: string) => Promise<void>;
  onSettingsUpdated?: (s: SettingsData) => void;
}

export const BotDetailPanel: React.FC<BotDetailPanelProps> = ({
  open,
  detail,
  bots,
  settings,
  models: _models,
  routineRefreshSeq,
  lang,
  onClose,
  onToast,
  onConfirm,
  onBotUpdated,
  onBotDeleted,
  onReloadBots,
  onLoadModels,
  onRefreshDetail,
  onSettingsUpdated,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [techOpen, setTechOpen] = useState(false);

  // Inline edit form fields
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editAvatar, setEditAvatar] = useState('');
  const [editColor, setEditColor] = useState('');
  const [appearanceOpen, setAppearanceOpen] = useState(false);
  const [editPrivate, setEditPrivate] = useState(false);
  const [editInst, setEditInst] = useState('');
  const [editErr, setEditErr] = useState('');
  const [editSaving, setEditSaving] = useState(false);

  // Memories
  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memErr, setMemErr] = useState('');
  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memKind, setMemKind] = useState('fact');
  const [memText, setMemText] = useState('');
  const [memSaving, setMemSaving] = useState(false);

  // Routines
  const [routines, setRoutines] = useState<RoutineItem[]>([]);
  const [rtLoading, setRtLoading] = useState(false);
  const [rtErr, setRtErr] = useState('');
  const [rtFormOpen, setRtFormOpen] = useState(false);
  const [rtEditId, setRtEditId] = useState('');
  const [rtName, setRtName] = useState('');
  const [rtMode, setRtMode] = useState<'interval' | 'daily' | 'cron' | 'webhook'>('interval');
  const [rtEvery, setRtEvery] = useState('60');
  const [rtDaily, setRtDaily] = useState('09:00');
  const [rtCron, setRtCron] = useState('0 9 * * 1-5');
  const [rtWebhookTrigger, setRtWebhookTrigger] = useState(false);
  const [rtWebhookSecret, setRtWebhookSecret] = useState('');
  const [rtWebhookSecretSet, setRtWebhookSecretSet] = useState(false);
  const [rtClearSecret, setRtClearSecret] = useState(false);
  const [rtWebhookUrl, setRtWebhookUrl] = useState('');
  const [rtDelivery, setRtDelivery] = useState('receipt');
  const [rtPrompt, setRtPrompt] = useState('');
  const [rtEnabled, setRtEnabled] = useState(true);
  const [rtSaving, setRtSaving] = useState(false);
  const [runsOpenId, setRunsOpenId] = useState('');
  const [runsMap, setRunsMap] = useState<Record<string, RoutineRun[]>>({});

  const loadMemories = async (botId: string) => {
    setMemLoading(true);
    setMemErr('');
    try {
      const d = await api<MemoryResponse>(`/api/memory?botId=${encodeURIComponent(botId)}&limit=30`);
      setMemData(d);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setMemLoading(false);
    }
  };

  const loadRoutines = async (botId: string) => {
    if (!routines.length) setRtLoading(true);
    setRtErr('');
    try {
      const d = await api<{ routines?: RoutineItem[] }>(`/api/bots/${encodeURIComponent(botId)}/routines`);
      setRoutines(d.routines || []);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
    } finally {
      setRtLoading(false);
    }
  };

  useEffect(() => {
    if (!open || !detail) return;
    setTechOpen(false);
    setAppearanceOpen(false);
    setMemAddOpen(false);
    setMemErr('');
    setRtFormOpen(false);
    setRtErr('');
    setRunsOpenId('');
    void loadMemories(detail.id);
    void loadRoutines(detail.id);
    if (detail.harness) void onLoadModels(detail.harness);
  }, [open, detail?.id]);

  useEffect(() => {
    if (!open || !detail) return;
    setEditName(detail.name || '');
    setEditDesc(detail.description || '');
    setEditAvatar(detail.avatar || '');
    setEditColor(detail.color || '');
    setEditPrivate(Boolean(detail.privateSessions));
    setEditInst(detail.instructions || '');
    setEditErr('');
  }, [open, detail?.id, detail?.name, detail?.description, detail?.avatar, detail?.color, detail?.privateSessions, detail?.instructions]);

  useEffect(() => {
    if (open && detail && routineRefreshSeq > 0) {
      void loadRoutines(detail.id);
    }
  }, [routineRefreshSeq]);

  const botAccentStyle: React.CSSProperties | undefined = detail
    ? ({
        '--bot-accent': botColor(detail.id, bots, editColor),
        '--bot-accent-text': contrastText(botColor(detail.id, bots, editColor)),
      } as React.CSSProperties)
    : undefined;

  const harnessLabel = (id?: string) =>
    settings?.harnesses.find((h) => h.id === id)?.label || id || 'opencode';

  const isDirty = Boolean(
    detail &&
      (editName !== (detail.name || '') ||
        editDesc !== (detail.description || '') ||
        editAvatar !== (detail.avatar || '') ||
        editColor !== (detail.color || '') ||
        editPrivate !== Boolean(detail.privateSessions) ||
        editInst !== (detail.instructions || ''))
  );

  const saveEdit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!detail) return;
    const name = editName.trim();
    if (!name) {
      setEditErr(t('nameRequired'));
      return;
    }
    setEditSaving(true);
    setEditErr('');
    try {
      const st = await api<BotStatus>(`/api/bots/${encodeURIComponent(detail.id)}`, {
        method: 'PATCH',
        body: {
          name,
          description: editDesc.trim(),
          avatar: editAvatar,
          color: editColor,
          instructions: editInst,
          privateSessions: editPrivate,
        },
      });
      onBotUpdated(st);
      await onRefreshDetail(detail.id);
      onToast(t('saved'));
    } catch (err) {
      if (err instanceof Error && err.message !== 'unauthorized') setEditErr(err.message);
    } finally {
      setEditSaving(false);
    }
  };

  const openRoutineForm = (rt?: RoutineItem) => {
    setRtErr('');
    setRtEditId(rt?.id || '');
    setRtName(rt?.name || '');
    const hasEvery = (rt?.everyMinutes || 0) > 0;
    const hasDaily = Boolean(rt?.dailyAt);
    const hasCron = Boolean(rt?.cron);
    let mode: 'interval' | 'daily' | 'cron' | 'webhook' = 'interval';
    if (hasCron) mode = 'cron';
    else if (hasDaily && !hasEvery) mode = 'daily';
    else if (!hasEvery && !hasDaily && rt?.webhookTrigger) mode = 'webhook';
    setRtMode(mode);
    setRtEvery(hasEvery ? String(rt?.everyMinutes) : '60');
    setRtDaily(rt?.dailyAt || '09:00');
    setRtCron(rt?.cron || '0 9 * * 1-5');
    setRtWebhookTrigger(Boolean(rt?.webhookTrigger) || mode === 'webhook');
    setRtWebhookSecret('');
    setRtWebhookSecretSet(Boolean(rt?.webhookSecretSet));
    setRtClearSecret(false);
    setRtWebhookUrl(rt?.webhookUrl || '');
    setRtDelivery(rt?.mainDelivery || 'receipt');
    setRtPrompt(rt?.prompt || '');
    setRtEnabled(rt ? rt.enabled !== false : true);
    setRtFormOpen(true);
  };

  const saveRoutine = async () => {
    if (!detail) return;
    const name = rtName.trim();
    const prompt = rtPrompt.trim();
    if (!name) {
      setRtErr(t('rtNameRequired'));
      return;
    }
    if (!prompt) {
      setRtErr(t('rtPromptRequired'));
      return;
    }
    const everyMinutes = rtMode === 'interval' ? parseInt(rtEvery, 10) : 0;
    const dailyAt = rtMode === 'daily' ? rtDaily.trim() : '';
    const cronExpr = rtMode === 'cron' ? rtCron.trim() : '';
    const webhookTrigger = rtMode === 'webhook' ? true : rtWebhookTrigger;
    if (rtMode === 'interval' && !(everyMinutes > 0)) {
      setRtErr(t('rtEveryPositive'));
      return;
    }
    if (rtMode === 'daily' && !/^\d{2}:\d{2}$/.test(dailyAt)) {
      setRtErr(t('rtTimeFormatErr'));
      return;
    }
    if (rtMode === 'cron' && !cronExpr) {
      setRtErr(t('rtCronRequired'));
      return;
    }

    setRtSaving(true);
    setRtErr('');
    const body: Record<string, unknown> = {
      name,
      everyMinutes,
      dailyAt,
      cron: cronExpr,
      webhookTrigger,
      webhookUrl: rtWebhookUrl.trim(),
      prompt,
      mainDelivery: rtDelivery,
      enabled: rtEnabled,
    };
    if (rtClearSecret) {
      body.clearWebhookSecret = true;
    } else if (rtWebhookSecret.trim()) {
      body.webhookSecret = rtWebhookSecret.trim();
    }
    try {
      if (rtEditId) {
        await api(`/api/bots/${encodeURIComponent(detail.id)}/routines/${encodeURIComponent(rtEditId)}`, {
          method: 'PATCH',
          body,
        });
        onToast(t('rtUpdated'));
      } else {
        await api(`/api/bots/${encodeURIComponent(detail.id)}/routines`, {
          method: 'POST',
          body,
        });
        onToast(t('rtCreated'));
      }
      setRtFormOpen(false);
      await loadRoutines(detail.id);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
    } finally {
      setRtSaving(false);
    }
  };

  const handleSaveMemory = async () => {
    if (!detail) return;
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
        body: { botId: detail.id, kind: memKind, text },
      });
      setMemText('');
      setMemAddOpen(false);
      onToast(t('botMemSaved'));
      await loadMemories(detail.id);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setMemSaving(false);
    }
  };

  const handleDeleteMemory = async (memId: string) => {
    if (!detail) return;
    setMemErr('');
    try {
      await api(`/api/memory/${encodeURIComponent(memId)}`, { method: 'DELETE' });
      onToast(t('memDeleted'));
      await loadMemories(detail.id);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    }
  };

  return (
    <aside id="panel" className={`panel${open ? ' open' : ''}`} aria-label={t('botDetails')} style={botAccentStyle}>
      <div className="panel-hd">
        <h3 id="pTitle">{t('details')}</h3>
        <div className="side-acts">
          <button
            type="button"
            className="ghost"
            aria-label={t('close')}
            title={t('close')}
            onClick={onClose}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        </div>
      </div>

      <div className="panel-b">
        {detail && (
          <div id="pView">
            <div className="p-hero">
              <button
                type="button"
                id="pAv"
                className="p-av-btn"
                aria-label={t('appearance')}
                aria-expanded={appearanceOpen}
                onClick={() => setAppearanceOpen((v) => !v)}
              >
                <BotAvatar id={detail.id} bots={bots} avatar={editAvatar} color={editColor} />
              </button>
              <button
                type="button"
                id="pAppearToggle"
                className={`mp mb-6${appearanceOpen ? ' go' : ''}`}
                onClick={() => setAppearanceOpen((v) => !v)}
              >
                {t('appearance')}
              </button>
              {appearanceOpen && (
                <BotAppearancePicker
                  botId={detail.id}
                  bots={bots}
                  avatar={editAvatar}
                  color={editColor}
                  lang={lang}
                  onAvatarChange={setEditAvatar}
                  onColorChange={setEditColor}
                />
              )}
              <input
                id="pName"
                className="p-hero-name"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                aria-label={t('name')}
              />
              <input
                id="pDesc"
                className="p-hero-desc"
                value={editDesc}
                placeholder={t('descPh')}
                onChange={(e) => setEditDesc(e.target.value)}
                aria-label={t('descOptional')}
              />
            </div>
            <div className="stats">
              <div className="stat">
                <b id="pTurns">{detail.session?.turnCount || 0}</b>
                <span>{t('statTurns')}</span>
              </div>
              <div className="stat">
                <b id="pCost">${(detail.session?.totalCostUsd || 0).toFixed(2)}</b>
                <span>{t('statCost')}</span>
              </div>
              <div className="stat">
                <b id="pState">{detail.busy ? t('busy') : t('idle')}</b>
                <span>{t('statState')}</span>
              </div>
            </div>

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
                      await loadMemories(detail.id);
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

            <div className="sec row-between">
              <span>{t('routines')}</span>
              <button
                type="button"
                id="pRtAddToggle"
                className="mp go"
                onClick={() => {
                  if (!rtFormOpen || rtEditId) openRoutineForm();
                  else setRtFormOpen(false);
                }}
              >
                {t('addShort')}
              </button>
            </div>
            {rtFormOpen && (
              <div id="pRtFormBox" className="add-prov mem-add mb-8">
                <div className="fld">
                  <label>{t('name')}</label>
                  <input id="pRtName" placeholder={t('rtNamePh')} value={rtName} onChange={(e) => setRtName(e.target.value)} />
                </div>
                <div className="two">
                  <div className="fld">
                    <label>{t('rtTriggerMode')}</label>
                    <select
                      id="pRtMode"
                      value={rtMode}
                      onChange={(e) => {
                        const next = e.target.value as 'interval' | 'daily' | 'cron' | 'webhook';
                        setRtMode(next);
                        if (next === 'webhook') setRtWebhookTrigger(true);
                      }}
                    >
                      <option value="interval">{t('rtModeInterval')}</option>
                      <option value="daily">{t('rtModeDaily')}</option>
                      <option value="cron">{t('rtModeCron')}</option>
                      <option value="webhook">{t('rtModeWebhook')}</option>
                    </select>
                  </div>
                  {rtMode === 'interval' ? (
                    <div className="fld" id="pRtEveryWrap">
                      <label>{t('rtEveryMinutes')}</label>
                      <input
                        id="pRtEvery"
                        className="mono"
                        type="number"
                        min={1}
                        value={rtEvery}
                        onChange={(e) => setRtEvery(e.target.value)}
                      />
                    </div>
                  ) : rtMode === 'daily' ? (
                    <div className="fld" id="pRtDailyWrap">
                      <label>
                        {t('rtTime')} <em>HH:MM</em>
                      </label>
                      <input
                        id="pRtDaily"
                        className="mono"
                        placeholder="09:00"
                        value={rtDaily}
                        onChange={(e) => setRtDaily(e.target.value)}
                      />
                    </div>
                  ) : rtMode === 'cron' ? (
                    <div className="fld" id="pRtCronWrap">
                      <label>{t('rtCronExpr')}</label>
                      <input
                        id="pRtCron"
                        className="mono"
                        placeholder={t('rtCronPh')}
                        value={rtCron}
                        onChange={(e) => setRtCron(e.target.value)}
                      />
                      <div className="set-card-desc mt-4">{t('rtCronHint')}</div>
                    </div>
                  ) : (
                    <div className="fld" />
                  )}
                </div>
                {rtMode !== 'webhook' && (
                  <label className="perm perm-card mb-6">
                    <input
                      type="checkbox"
                      id="pRtWebhookTrigger"
                      checked={rtWebhookTrigger}
                      onChange={(e) => setRtWebhookTrigger(e.target.checked)}
                    />
                    {t('rtAllowWebhook')}
                  </label>
                )}
                {(rtMode === 'webhook' || rtWebhookTrigger) && (
                  <div className="fld">
                    <label>
                      {t('rtWebhookSecret')} <em>{t('rtOptional')}</em>
                    </label>
                    <input
                      id="pRtWebhookSecret"
                      type="password"
                      className="mono"
                      placeholder={rtWebhookSecretSet && !rtClearSecret ? t('rtWebhookSecretSetPh') : t('rtWebhookSecretPh')}
                      value={rtWebhookSecret}
                      disabled={rtClearSecret}
                      onChange={(e) => setRtWebhookSecret(e.target.value)}
                    />
                    {rtWebhookSecretSet && (
                      <label className="perm perm-card mt-4">
                        <input
                          type="checkbox"
                          checked={rtClearSecret}
                          onChange={(e) => {
                            setRtClearSecret(e.target.checked);
                            if (e.target.checked) setRtWebhookSecret('');
                          }}
                        />
                        {t('rtWebhookClearSecret')}
                      </label>
                    )}
                  </div>
                )}
                <div className="fld">
                  <label>
                    {t('rtWebhookUrl')} <em>{t('rtOptional')}</em>
                  </label>
                  <input
                    id="pRtWebhookUrl"
                    className="mono"
                    placeholder={t('rtWebhookUrlPh')}
                    value={rtWebhookUrl}
                    onChange={(e) => setRtWebhookUrl(e.target.value)}
                  />
                </div>
                <div className="fld">
                  <label>{t('rtMainDelivery')}</label>
                  <select id="pRtDelivery" value={rtDelivery} onChange={(e) => setRtDelivery(e.target.value)}>
                    <option value="receipt">{t('rtDeliveryReceiptFull')}</option>
                    <option value="notify">{t('rtDeliveryNotifyFull')}</option>
                    <option value="none">{t('rtDeliveryNoneFull')}</option>
                  </select>
                </div>
                <div className="fld">
                  <label>{t('rtPrompt')}</label>
                  <textarea
                    id="pRtPrompt"
                    rows={3}
                    className="ta-sm ta-font"
                    placeholder={t('rtPromptPh')}
                    value={rtPrompt}
                    onChange={(e) => setRtPrompt(e.target.value)}
                  />
                </div>
                <div className="row-between row-wrap">
                  <label className="perm perm-card">
                    <input
                      type="checkbox"
                      id="pRtEnabled"
                      checked={rtEnabled}
                      onChange={(e) => setRtEnabled(e.target.checked)}
                    />
                    {t('rtEnabled')}
                  </label>
                  <div className="row-inline-sm">
                    <button
                      type="button"
                      id="pRtCancel"
                      className="mp"
                      onClick={() => {
                        setRtFormOpen(false);
                        setRtErr('');
                      }}
                    >
                      {t('cancel')}
                    </button>
                    <button
                      type="button"
                      id="pRtSave"
                      className="mp go"
                      disabled={rtSaving}
                      onClick={() => void saveRoutine()}
                    >
                      {t('save')}
                    </button>
                  </div>
                </div>
              </div>
            )}

            <div id="pRtList" className="stack6">
              {rtLoading && routines.length === 0 ? (
                <div className="set-card-desc">{t('loadingEllipsis')}</div>
              ) : routines.length === 0 ? (
                <div className="set-card-desc">{t('rtEmpty')}</div>
              ) : (
                routines.map((rt) => {
                  const trig = [
                    (rt.everyMinutes || 0) > 0 ? t('rtEveryShort', { n: rt.everyMinutes || 0 }) : '',
                    rt.dailyAt ? t('rtDailyShort', { time: rt.dailyAt }) : '',
                    rt.cron ? `Cron ${rt.cron}` : '',
                    rt.webhookTrigger ? t('rtWebhookTag') : '',
                  ]
                    .filter(Boolean)
                    .join(' · ');
                  const delivLbl =
                    rt.mainDelivery === 'notify'
                      ? t('rtDeliveryNotify')
                      : rt.mainDelivery === 'none'
                        ? t('rtDeliveryNone')
                        : t('rtDeliveryReceipt');
                  const nextTxt =
                    rt.enabled && rt.nextRunAt
                      ? t('rtNextAt', {
                          time: new Date(rt.nextRunAt).toLocaleTimeString(lang === 'en' ? 'en-US' : 'zh-CN', {
                            hour: '2-digit',
                            minute: '2-digit',
                            second: rt.cron ? '2-digit' : undefined,
                          }),
                        })
                      : rt.enabled
                        ? ''
                        : t('rtDisabled');
                  const lastTxt = rt.lastRun
                    ? `${formatStamp(rt.lastRun.finishedAt, lang)} · ${(rt.lastRun.durationMs / 1000).toFixed(1)}s`
                    : '';
                  const meta = [trig, delivLbl, rt.webhookUrl ? t('rtWebhookPushTag') : '', rt.model, nextTxt, lastTxt]
                    .filter(Boolean)
                    .join(' · ');
                  const runsOpen = runsOpenId === rt.id;
                  const runs = runsMap[rt.id] || [];

                  return (
                    <div key={rt.id} className="mem-row">
                      <div className="mem-top">
                        <div className="mem-meta">
                          <span className="rt-title">{rt.name}</span>
                          {rt.running ? (
                            <span className="mp ok">{t('rtStatusRunning')}</span>
                          ) : rt.lastRun?.status === 'ok' ? (
                            <span className="mp ok">{t('rtStatusOk')}</span>
                          ) : rt.lastRun?.status === 'silent' ? (
                            <span className="mp">{t('rtStatusSilent')}</span>
                          ) : rt.lastRun?.status === 'error' ? (
                            <span className="mp bad">{t('rtStatusError')}</span>
                          ) : null}
                        </div>
                        <div className="prov-acts">
                          <button
                            type="button"
                            className="mp"
                            onClick={async () => {
                              setRtErr('');
                              try {
                                await api(`/api/bots/${encodeURIComponent(detail.id)}/routines/${encodeURIComponent(rt.id)}`, {
                                  method: 'PATCH',
                                  body: { enabled: !rt.enabled },
                                });
                                await loadRoutines(detail.id);
                              } catch (e) {
                                if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
                              }
                            }}
                          >
                            {rt.enabled ? t('rtDisableBtn') : t('rtEnableBtn')}
                          </button>
                          <button
                            type="button"
                            className="mp"
                            disabled={rt.running}
                            onClick={async () => {
                              setRtErr('');
                              try {
                                await api(
                                  `/api/bots/${encodeURIComponent(detail.id)}/routines/${encodeURIComponent(rt.id)}/run?async=1`,
                                  { method: 'POST' }
                                );
                                onToast(t('rtTriggered', { name: rt.name }));
                                await loadRoutines(detail.id);
                              } catch (e) {
                                if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
                              }
                            }}
                          >
                            {rt.running ? t('rtRunningBtn') : t('rtRunBtn')}
                          </button>
                          <button
                            type="button"
                            className="mp"
                            onClick={async () => {
                              if (runsOpen) {
                                setRunsOpenId('');
                                return;
                              }
                              setRunsOpenId(rt.id);
                              try {
                                const d = await api<{ runs?: RoutineRun[] }>(
                                  `/api/bots/${encodeURIComponent(detail.id)}/routine-runs?routineId=${encodeURIComponent(rt.id)}&limit=8`
                                );
                                setRunsMap((prev) => ({ ...prev, [rt.id]: d.runs || [] }));
                              } catch {}
                            }}
                          >
                            {t('rtRunsBtn')}
                          </button>
                          <button type="button" className="mp" onClick={() => openRoutineForm(rt)}>
                            {t('rtEditBtn')}
                          </button>
                          {rt.webhookTrigger && rt.webhookPath && (
                            <button
                              type="button"
                              className="mp"
                              onClick={() => {
                                const fullUrl = `${window.location.origin}${rt.webhookPath}`;
                                void navigator.clipboard?.writeText(fullUrl).then(() => onToast(t('rtWebhookCopied')));
                              }}
                            >
                              {t('rtCopyWebhook')}
                            </button>
                          )}
                          <button
                            type="button"
                            className="mp bad"
                            onClick={() => {
                              onConfirm({
                                title: t('rtDeleteTitle', { name: rt.name }),
                                text: t('rtDeleteDesc'),
                                okText: t('delBtn'),
                                okClass: 'bad',
                                onOk: async () => {
                                  await api(
                                    `/api/bots/${encodeURIComponent(detail.id)}/routines/${encodeURIComponent(rt.id)}`,
                                    { method: 'DELETE' }
                                  );
                                  onToast(t('rtDeleted'));
                                  await loadRoutines(detail.id);
                                },
                              });
                            }}
                          >
                            {t('delBtn')}
                          </button>
                        </div>
                      </div>
                      <div className="mem-time">{meta}</div>
                      <div className="mem-txt">{rt.prompt}</div>
                      {runsOpen && (
                        <div className="rt-runs">
                          {runs.length === 0 ? (
                            <div className="set-card-desc">{t('rtRunsEmpty')}</div>
                          ) : (
                            runs.map((r, idx) => {
                              const stText =
                                r.status === 'ok'
                                  ? t('rtStatusOk')
                                  : r.status === 'silent'
                                    ? t('rtStatusSilent')
                                    : t('rtStatusError');
                              const costTxt = r.costUsd ? ` · $${Number(r.costUsd).toFixed(4)}` : '';
                              return (
                                <div key={r.id || idx} className="rt-run">
                                  <div className="rt-run-hd">
                                    <span>
                                      {stText} · {r.trigger} · {(r.durationMs / 1000).toFixed(1)}s{costTxt}
                                    </span>
                                    <span>{formatStamp(r.finishedAt, lang)}</span>
                                  </div>
                                  {r.error ? (
                                    <div className="rt-run-err">{r.error}</div>
                                  ) : r.output ? (
                                    <div>
                                      {r.output.slice(0, 180)}
                                      {r.output.length > 180 ? '…' : ''}
                                    </div>
                                  ) : null}
                                </div>
                              );
                            })
                          )}
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>
            <div id="pRtErr" className="msg-err">
              {rtErr}
            </div>

            <details
              id="pTechDetails"
              className="p-tech"
              open={techOpen}
              onToggle={(e) => setTechOpen(e.currentTarget.open)}
            >
              <summary>
                <span>{t('techDetails')}</span>
                <svg
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="m9 18 6-6-6-6" />
                </svg>
              </summary>
              <div className="p-tech-body">
                <dl className="kv">
                  <div>
                    <dt>ID</dt>
                    <dd id="pId">{detail.id}</dd>
                  </div>
                  <div>
                    <dt>Harness</dt>
                    <dd id="pHarness">{harnessLabel(detail.harness)}</dd>
                  </div>
                  <div>
                    <dt>{t('model')}</dt>
                    <dd id="pModel">{(detail.model || '—') + (detail.modelInherited ? t('defaultTag') : '')}</dd>
                  </div>
                  <div>
                    <dt>Session</dt>
                    <dd id="pSession">{detail.session?.sessionId || '—'}</dd>
                  </div>
                  <div>
                    <dt>{t('archivedSessions')}</dt>
                    <dd id="pArchived">
                      {t('archivedCount', { n: detail.archivedSessions || 0 }) +
                        (detail.privateSessions ? t('privateTag') : '')}
                    </dd>
                  </div>
                  <div>
                    <dt>Workspace</dt>
                    <dd id="pWs">{detail.workspace}</dd>
                  </div>
                </dl>
                <label className="perm self-start mt-10">
                  <input
                    type="checkbox"
                    id="pePrivate"
                    checked={editPrivate}
                    onChange={(e) => setEditPrivate(e.target.checked)}
                  />
                  <span>{t('privateSessionsLabel')}</span>
                </label>
                <div className="sec">AGENTS.md</div>
                <textarea
                  id="pInst"
                  className="agents p-agents-in"
                  rows={10}
                  spellCheck={false}
                  placeholder={t('agentsPh')}
                  value={editInst}
                  onChange={(e) => setEditInst(e.target.value)}
                />
              </div>
            </details>
          </div>
        )}
      </div>

      {editErr && (
        <div id="peErr" className="msg-err mt-6">
          {editErr}
        </div>
      )}

      <div className="panel-f">
        <div id="pViewFoot" className="panel-foot-row">
          <button
            type="button"
            id="pReset"
            className="btn"
            onClick={() => {
              if (!detail) return;
              onClose();
              onConfirm({
                title: t('resetSessionTitle'),
                text: t('resetSessionDesc', { name: detail.name }),
                okText: t('resetBtn'),
                onOk: async () => {
                  await api(`/api/bots/${encodeURIComponent(detail.id)}/reset`, { method: 'POST' });
                  onToast(t('newSessionStarted'));
                  await onReloadBots();
                },
              });
            }}
          >
            {t('resetSession')}
          </button>
          {isDirty ? (
            <button
              type="button"
              id="peSave"
              className="btn go"
              disabled={editSaving}
              onClick={() => void saveEdit()}
            >
              {t('save')}
            </button>
          ) : (
            <button
              type="button"
              id="pDelete"
              className="btn ghost-bad"
              disabled={bots.length <= 1}
              title={bots.length <= 1 ? t('keepOneBot') : ''}
              onClick={() => {
                if (!detail || bots.length <= 1) return;
                onConfirm({
                  title: t('deleteBotTitle', { name: detail.name }),
                  text: t('deleteBotDesc'),
                  okText: t('confirmDelete'),
                  okClass: 'bad',
                  onOk: async () => {
                    await api(`/api/bots/${encodeURIComponent(detail.id)}`, { method: 'DELETE' });
                    onClose();
                    onBotDeleted(detail.id);
                    onToast(t('deletedBot'));
                  },
                });
              }}
            >
              {t('deleteBot')}
            </button>
          )}
        </div>
      </div>
    </aside>
  );
};
