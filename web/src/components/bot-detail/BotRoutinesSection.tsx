import React, { useEffect, useState } from 'react';
import type { ConfirmOptions, ResolvedLang, RoutineItem, RoutineRun } from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { formatStamp } from '../../lib/theme';

interface BotRoutinesSectionProps {
  open: boolean;
  botId: string;
  routineRefreshSeq: number;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
}

export const BotRoutinesSection: React.FC<BotRoutinesSectionProps> = ({
  open,
  botId,
  routineRefreshSeq,
  lang,
  onToast,
  onConfirm,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
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
  const [rtPermMode, setRtPermMode] = useState('');
  const [rtPrompt, setRtPrompt] = useState('');
  const [rtEnabled, setRtEnabled] = useState(true);
  const [rtSaving, setRtSaving] = useState(false);
  const [runsOpenId, setRunsOpenId] = useState('');
  const [runsMap, setRunsMap] = useState<Record<string, RoutineRun[]>>({});

  const loadRoutines = async (targetBotId: string) => {
    if (!routines.length) setRtLoading(true);
    setRtErr('');
    try {
      const d = await api<{ routines?: RoutineItem[] }>(`/api/bots/${encodeURIComponent(targetBotId)}/routines`);
      setRoutines(d.routines || []);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
    } finally {
      setRtLoading(false);
    }
  };

  useEffect(() => {
    if (!open || !botId) return;
    setRtFormOpen(false);
    setRtErr('');
    setRunsOpenId('');
    void loadRoutines(botId);
  }, [open, botId]);

  useEffect(() => {
    if (open && botId && routineRefreshSeq > 0) {
      void loadRoutines(botId);
    }
  }, [routineRefreshSeq, open, botId]);

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
    setRtPermMode(rt?.permissionMode || '');
    setRtPrompt(rt?.prompt || '');
    setRtEnabled(rt ? rt.enabled !== false : true);
    setRtFormOpen(true);
  };

  const saveRoutine = async () => {
    if (!botId) return;
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
      permissionMode: rtPermMode,
      enabled: rtEnabled,
    };
    if (rtClearSecret) {
      body.clearWebhookSecret = true;
    } else if (rtWebhookSecret.trim()) {
      body.webhookSecret = rtWebhookSecret.trim();
    }
    try {
      if (rtEditId) {
        await api(`/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rtEditId)}`, {
          method: 'PATCH',
          body,
        });
        onToast(t('rtUpdated'));
      } else {
        await api(`/api/bots/${encodeURIComponent(botId)}/routines`, {
          method: 'POST',
          body,
        });
        onToast(t('rtCreated'));
      }
      setRtFormOpen(false);
      await loadRoutines(botId);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setRtErr(e.message);
    } finally {
      setRtSaving(false);
    }
  };

  return (
    <>
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
        <div className="two">
          <div className="fld">
            <label>{t('rtMainDelivery')}</label>
            <select id="pRtDelivery" value={rtDelivery} onChange={(e) => setRtDelivery(e.target.value)}>
              <option value="receipt">{t('rtDeliveryReceiptFull')}</option>
              <option value="notify">{t('rtDeliveryNotifyFull')}</option>
              <option value="none">{t('rtDeliveryNoneFull')}</option>
            </select>
          </div>
          <div className="fld">
            <label>{t('rtPermMode')}</label>
            <select id="pRtPermMode" value={rtPermMode} onChange={(e) => setRtPermMode(e.target.value)}>
              <option value="">{t('rtPermFollow')}</option>
              <option value="auto">{t('rtPermAuto')}</option>
              <option value="plan">{t('rtPermPlan')}</option>
            </select>
          </div>
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
          const meta = [
            trig,
            delivLbl,
            rt.permissionMode ? t(`permMode_${rt.permissionMode}`) : '',
            rt.webhookUrl ? t('rtWebhookPushTag') : '',
            rt.model,
            nextTxt,
            lastTxt,
          ]
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
                        await api(`/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rt.id)}`, {
                          method: 'PATCH',
                          body: { enabled: !rt.enabled },
                        });
                        await loadRoutines(botId);
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
                          `/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rt.id)}/run?async=1`,
                          { method: 'POST' }
                        );
                        onToast(t('rtTriggered', { name: rt.name }));
                        await loadRoutines(botId);
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
                          `/api/bots/${encodeURIComponent(botId)}/routine-runs?routineId=${encodeURIComponent(rt.id)}&limit=8`
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
                            `/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rt.id)}`,
                            { method: 'DELETE' }
                          );
                          onToast(t('rtDeleted'));
                          await loadRoutines(botId);
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
    </>
  );
};
