import React, { useEffect, useState } from 'react';
import type { BotStatus, ConfirmOptions, PermissionMode, ResolvedLang, SettingsData } from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';
import { BotAppearancePicker } from './BotAppearancePicker';
import { BotAvatar } from './BotAvatar';

interface CreateBotModalProps {
  open: boolean;
  bots: BotStatus[];
  settings: SettingsData | null;
  models: Record<string, string[]>;
  lang: ResolvedLang;
  onClose: () => void;
  onCreated: (st: BotStatus) => Promise<void>;
  onLoadModels: (harness: string) => Promise<unknown>;
}

export const CreateBotModal: React.FC<CreateBotModalProps> = ({
  open,
  bots,
  settings,
  models,
  lang,
  onClose,
  onCreated,
  onLoadModels,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [name, setName] = useState('');
  const [id, setId] = useState('');
  const [idTouched, setIdTouched] = useState(false);
  const [desc, setDesc] = useState('');
  const [avatar, setAvatar] = useState('');
  const [color, setColor] = useState('');
  const [harness, setHarness] = useState('opencode');
  const [model, setModel] = useState('');
  const [permMode, setPermMode] = useState<PermissionMode>('auto');
  const [inst, setInst] = useState('');
  const [priv, setPriv] = useState(false);
  const [err, setErr] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    const defaultH = settings?.defaultHarness || settings?.harnesses?.[0]?.id || 'opencode';
    setName('');
    setId('');
    setIdTouched(false);
    setDesc('');
    setAvatar('');
    setColor('');
    setHarness(defaultH);
    setModel('');
    const hModes = settings?.harnesses.find((h) => h.id === defaultH)?.permissions?.modes;
    const defPerm = (settings?.defaultPermissionMode || 'auto') as PermissionMode;
    setPermMode(hModes && hModes.length > 0 && !hModes.includes(defPerm) ? hModes[0] : defPerm);
    setInst('');
    setPriv(false);
    setErr('');
    void onLoadModels(defaultH);
  }, [open]);

  const selectedHarness = settings?.harnesses.find((h) => h.id === harness);
  const defModel = selectedHarness?.defaultModel || '';
  const modelList = models[harness] || [];
  const supportedModes: PermissionMode[] =
    selectedHarness?.permissions?.modes && selectedHarness.permissions.modes.length > 0
      ? selectedHarness.permissions.modes
      : ['auto', 'edit', 'ask', 'plan'];
  const effectivePermMode: PermissionMode = supportedModes.includes(permMode)
    ? permMode
    : supportedModes[0] || 'auto';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setErr('');
    try {
      const st = await api<BotStatus>('/api/bots', {
        method: 'POST',
        body: {
          id: id.trim(),
          name: name.trim(),
          description: desc.trim(),
          avatar,
          color,
          harness,
          model: model.trim(),
          permissionMode: effectivePermMode,
          instructions: inst.trim(),
          privateSessions: priv,
        },
      });
      await onCreated(st);
    } catch (error) {
      setErr(error instanceof Error ? error.message : String(error));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      id="createDlg"
      className={`dlg-wrap${open ? ' open' : ''}`}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <form id="createForm" className="dlg" autoComplete="off" onSubmit={handleSubmit}>
        <div className="dlg-h">
          <div id="cAv">
            <BotAvatar id={id || 'new'} bots={bots} avatar={avatar} color={color} />
          </div>
          <div>
            <h3>{t('newBot')}</h3>
            <p>{t('newBotSub')}</p>
          </div>
        </div>
        <div className="dlg-b">
          <div className="two">
            <div className="fld">
              <label>{t('name')}</label>
              <input
                id="fName"
                placeholder="Nix · Infra"
                required
                value={name}
                onChange={(e) => {
                  const v = e.target.value;
                  setName(v);
                  if (!idTouched) {
                    setId(
                      v
                        .trim()
                        .toLowerCase()
                        .replace(/[^a-z0-9_-]+/g, '-')
                        .replace(/^-+|-+$/g, '')
                    );
                  }
                }}
              />
            </div>
            <div className="fld">
              <label>
                ID <em>a-z 0-9 - _</em>
              </label>
              <input
                id="fId"
                className="mono"
                placeholder="nix"
                pattern="[A-Za-z0-9_-]+"
                required
                spellCheck={false}
                value={id}
                onChange={(e) => {
                  setIdTouched(true);
                  setId(e.target.value);
                }}
              />
            </div>
          </div>
          <div className="fld">
            <label dangerouslySetInnerHTML={{ __html: t('descOptional') }} />
            <input id="fDesc" placeholder={t('descPh')} value={desc} onChange={(e) => setDesc(e.target.value)} />
          </div>
          <BotAppearancePicker
            botId={id || 'new'}
            bots={bots}
            avatar={avatar}
            color={color}
            lang={lang}
            onAvatarChange={setAvatar}
            onColorChange={setColor}
          />
          <div className="two">
            <div className="fld">
              <label>Harness</label>
              <select
                id="fHarness"
                value={harness}
                onChange={(e) => {
                  const nextH = e.target.value;
                  setHarness(nextH);
                  setModel('');
                  void onLoadModels(nextH);
                }}
              >
                {(settings?.harnesses || [{ id: 'opencode', label: 'opencode' }]).map((h) => (
                  <option key={h.id} value={h.id}>
                    {h.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="fld">
              <label dangerouslySetInnerHTML={{ __html: t('modelDefaultOptional') }} />
              <input
                id="fModel"
                className="mono"
                list="createModelsList"
                placeholder={defModel ? t('defaultModelPh', { model: defModel }) : 'provider/model'}
                spellCheck={false}
                value={model}
                onFocus={() => void onLoadModels(harness)}
                onChange={(e) => setModel(e.target.value)}
              />
              <datalist id="createModelsList">
                {modelList.map((m) => (
                  <option key={m} value={m} />
                ))}
              </datalist>
            </div>
          </div>
          <div className="fld">
            <label htmlFor="fPermMode">{t('permMode')}</label>
            <select
              id="fPermMode"
              disabled={supportedModes.length <= 1}
              value={effectivePermMode}
              onChange={(e) => setPermMode(e.target.value as PermissionMode)}
            >
              {supportedModes.map((m) => (
                <option key={m} value={m}>
                  {t(`permMode_${m}`)}
                </option>
              ))}
            </select>
            <div className="set-card-desc mt-4">
              {supportedModes.length <= 1
                ? t('permModeUnsupported', { mode: t(`permMode_${supportedModes[0] || 'auto'}`) })
                : t(`permDesc_${effectivePermMode}`)}
            </div>
          </div>
          <div className="fld">
            <label dangerouslySetInnerHTML={{ __html: t('agentsAutoOptional') }} />
            <textarea
              id="fInst"
              placeholder={t('agentsCreatePh')}
              spellCheck={false}
              value={inst}
              onChange={(e) => setInst(e.target.value)}
            />
          </div>
          <label className="perm self-start">
            <input type="checkbox" id="fPrivate" checked={priv} onChange={(e) => setPriv(e.target.checked)} />
            <span>{t('privateSessionsLabel')}</span>
          </label>
          <div id="createErr" className="msg-err">
            {err}
          </div>
        </div>
        <div className="dlg-f">
          <button type="button" className="btn" onClick={onClose}>
            {t('cancel')}
          </button>
          <button type="submit" id="createSubmit" className="btn go" disabled={submitting}>
            {t('create')}
          </button>
        </div>
      </form>
    </div>
  );
};

interface ConfirmModalProps {
  options: ConfirmOptions | null;
  lang: ResolvedLang;
  onClose: () => void;
}

export const ConfirmModal: React.FC<ConfirmModalProps> = ({ options, lang, onClose }) => {
  const t = (k: string) => translate(lang, k);
  const [running, setRunning] = useState(false);

  return (
    <div
      id="confirmDlg"
      className={`dlg-wrap${options ? ' open' : ''}`}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="dlg sm">
        <div className="dlg-h">
          <div>
            <h3 id="confirmTitle">{options?.title || t('confirmAction')}</h3>
          </div>
        </div>
        <div className="dlg-b">
          <p id="confirmText">{options?.text || ''}</p>
        </div>
        <div className="dlg-f">
          <button type="button" id="confirmCancel" className="btn" onClick={onClose}>
            {t('cancel')}
          </button>
          {options?.altText && options?.onAlt && (
            <button
              type="button"
              id="confirmAlt"
              className={`btn ${options.altClass ?? ''}`.trim()}
              disabled={running}
              onClick={async () => {
                const fn = options.onAlt!;
                onClose();
                setRunning(true);
                try {
                  await fn();
                } finally {
                  setRunning(false);
                }
              }}
            >
              {options.altText}
            </button>
          )}
          <button
            type="button"
            id="confirmOk"
            className={`btn ${options?.okClass ?? 'bad'}`}
            disabled={running}
            onClick={async () => {
              if (!options) return;
              const fn = options.onOk;
              onClose();
              setRunning(true);
              try {
                await fn();
              } finally {
                setRunning(false);
              }
            }}
          >
            {options?.okText || t('ok')}
          </button>
        </div>
      </div>
    </div>
  );
};

interface LoginModalProps {
  open: boolean;
  lang: ResolvedLang;
  onSuccess: () => Promise<void>;
}

export const LoginModal: React.FC<LoginModalProps> = ({ open, lang, onSuccess }) => {
  const t = (k: string) => translate(lang, k);
  const [token, setToken] = useState('');
  const [err, setErr] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr('');
    try {
      await api('/api/login', { method: 'POST', body: { token } });
      setToken('');
      await onSuccess();
    } catch (error) {
      setErr(error instanceof Error ? error.message : String(error));
    }
  };

  return (
    <div id="loginDlg" className={`dlg-wrap${open ? ' open' : ''}`}>
      <form id="loginForm" className="dlg sm" onSubmit={handleSubmit}>
        <div className="dlg-h">
          <div>
            <h3>{t('tokenRequired')}</h3>
            <p>{t('tokenHint')}</p>
          </div>
        </div>
        <div className="dlg-b">
          <div className="fld">
            <input
              id="loginToken"
              className="mono"
              type="password"
              placeholder="token"
              autoComplete="current-password"
              required
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
          </div>
          <div id="loginErr" className="msg-err">
            {err}
          </div>
        </div>
        <div className="dlg-f">
          <button type="submit" className="btn go">
            {t('enter')}
          </button>
        </div>
      </form>
    </div>
  );
};
