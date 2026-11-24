import React, { useEffect, useState } from 'react';
import type { HarnessInfo, ResolvedLang, SettingsData } from '../../types';
import { api } from '../../lib/api';
import { MagpieInstallBanner } from '../MagpieInstallBanner';

interface StepMagpieProps {
  t: (k: string, vars?: Record<string, string | number>) => string;
  lang: ResolvedLang;
  settings: SettingsData | null;
  selectedHarness: string;
  hLabel: string;
  hInfo: HarnessInfo | undefined;
  defaultModel: string;
  onChangeDefaultModel: (v: string) => void;
  busy: boolean;
  onBusyChange: (b: boolean) => void;
  onRecheck: () => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onLoadSettings: (fresh?: boolean) => Promise<SettingsData>;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onResetModels: () => void;
  onToast: (msg: string) => void;
  onError: (msg: string) => void;
}

export const StepMagpie: React.FC<StepMagpieProps> = ({
  t,
  lang,
  settings,
  selectedHarness,
  hLabel,
  hInfo,
  defaultModel,
  onChangeDefaultModel,
  busy,
  onBusyChange,
  onRecheck,
  onSettingsUpdated,
  onLoadSettings,
  onLoadModels,
  onResetModels,
  onToast,
  onError,
}) => {
  const [provDismissed, setProvDismissed] = useState(false);
  const [addProvOpen, setAddProvOpen] = useState(false);
  const [apKind, setApKind] = useState<'preset' | 'custom'>('preset');
  const [apPreset, setApPreset] = useState('');
  const [apProtocol, setApProtocol] = useState('openai');
  const [apName, setApName] = useState('');
  const [apKey, setApKey] = useState('');
  const [apUrl, setApUrl] = useState('');
  const [apModels, setApModels] = useState('');
  const [apSubmitting, setApSubmitting] = useState(false);

  const mp = settings?.magpie;
  const provs = mp?.providers || [];
  const presets = mp?.presets || [];
  const showAddProv = Boolean(mp?.installed) && (addProvOpen || (provs.length === 0 && !provDismissed));

  useEffect(() => {
    if (presets[0] && (!apPreset || !presets.some((p) => p.id === apPreset))) {
      setApPreset(presets[0].id);
    }
  }, [presets, apPreset]);

  return (
    <div id="ubStep2" className="set-stack">
      <div id="ubMagpiePanel" className="note">
        {!mp?.installed ? (
          <MagpieInstallBanner
            lang={lang}
            hint={t('ubMagpieNotInstalledPlain', { label: hLabel })}
            installCmds={mp?.install}
            onToast={onToast}
            onInstalled={async () => {
              onResetModels();
              await onLoadSettings(true);
              await onLoadModels(selectedHarness, true);
            }}
          />
        ) : (
          <>
            <div className="row-between row-wrap">
              <div className="row-inline row-wrap">
                <strong>Magpie</strong>
                <span className={`mp ${mp.healthy ? 'ok' : 'bad'}`}>
                  {mp.healthy ? t('ubGatewayRunning') : t('ubGatewayNotStarted')}
                </span>
                <span className="mp">{t('ubProviderCount', { n: provs.length })}</span>
              </div>
              <div className="row-inline-sm">
                <button type="button" id="ubRecheck2" className="mp" disabled={busy} onClick={onRecheck}>
                  {t('ubRecheck')}
                </button>
                {(provs.length > 0 || provDismissed) && (
                  <button
                    type="button"
                    id="ubShowAddProv"
                    className="mp go"
                    onClick={() => {
                      setProvDismissed(false);
                      setAddProvOpen((v) => !v);
                    }}
                  >
                    {t('addProviderBtn')}
                  </button>
                )}
              </div>
            </div>
            <div>{provs.length ? t('ubHasProvidersDesc') : t('ubNoProvidersDesc')}</div>
          </>
        )}
      </div>

      {showAddProv && (
        <div id="ubAddProvBox" className="add-prov">
          <div className="two">
            <div className="fld">
              <label>{t('addMethod')}</label>
              <select
                id="ubApKind"
                value={apKind}
                onChange={(e) => setApKind(e.target.value as 'preset' | 'custom')}
              >
                <option value="preset">{t('presetVendor')}</option>
                <option value="custom">{t('customEndpoint')}</option>
              </select>
            </div>
            {apKind === 'preset' ? (
              <div className="fld" id="ubApPresetFld">
                <label>{t('presetVendor')}</label>
                <select id="ubApPreset" value={apPreset} onChange={(e) => setApPreset(e.target.value)}>
                  {presets.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} ({p.id})
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <div className="fld" id="ubApProtoFld">
                <label>{t('apiProtocol')}</label>
                <select id="ubApProtocol" value={apProtocol} onChange={(e) => setApProtocol(e.target.value)}>
                  <option value="openai">{t('protoOpenAI')}</option>
                  <option value="anthropic">{t('protoAnthropic')}</option>
                  <option value="responses">OpenAI Responses</option>
                  <option value="gemini">Google Gemini</option>
                </select>
              </div>
            )}
          </div>
          <div className="two">
            <div className="fld">
              {apKind === 'custom' ? (
                <label>{t('aliasRequired')}</label>
              ) : (
                <label dangerouslySetInnerHTML={{ __html: t('aliasOptional') }} />
              )}
              <input
                id="ubApName"
                placeholder={t('aliasPh')}
                value={apName}
                onChange={(e) => setApName(e.target.value)}
              />
            </div>
            <div className="fld">
              <label dangerouslySetInnerHTML={{ __html: t('apiKeyLabel') }} />
              <input
                id="ubApKey"
                type="password"
                className="mono"
                placeholder="sk-..."
                autoComplete="new-password"
                value={apKey}
                onChange={(e) => setApKey(e.target.value)}
              />
            </div>
          </div>
          {apKind === 'custom' && (
            <div id="ubApCustomRow" className="two">
              <div className="fld">
                <label>Base URL</label>
                <input
                  id="ubApUrl"
                  className="mono"
                  placeholder="https://api.example.com/v1"
                  value={apUrl}
                  onChange={(e) => setApUrl(e.target.value)}
                />
              </div>
              <div className="fld">
                <label dangerouslySetInnerHTML={{ __html: t('modelsListOptional') }} />
                <input
                  id="ubApModels"
                  className="mono"
                  placeholder="model-a,model-b"
                  value={apModels}
                  onChange={(e) => setApModels(e.target.value)}
                />
              </div>
            </div>
          )}
          <div className="row-end-sm">
            <button
              type="button"
              id="ubApCancel"
              className="mp"
              onClick={() => {
                setProvDismissed(true);
                setAddProvOpen(false);
              }}
            >
              {t('cancel')}
            </button>
            <button
              type="button"
              id="ubApSubmit"
              className="mp go"
              disabled={apSubmitting}
              onClick={async () => {
                const custom = apKind === 'custom';
                setApSubmitting(true);
                onError('');
                try {
                  await api('/api/magpie/providers', {
                    method: 'POST',
                    body: {
                      action: 'add',
                      preset: custom ? '' : apPreset,
                      name: apName.trim(),
                      protocol: custom ? apProtocol : '',
                      url: custom ? apUrl.trim() : '',
                      key: apKey.trim(),
                      models: custom ? apModels.trim() : '',
                    },
                  });
                  setApKey('');
                  setApName('');
                  setApUrl('');
                  setApModels('');
                  setProvDismissed(false);
                  setAddProvOpen(false);
                  onResetModels();
                  await onLoadSettings(true);
                  await onLoadModels(selectedHarness, true);
                  onToast(t('providerAdded'));
                } catch (e) {
                  if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
                } finally {
                  setApSubmitting(false);
                }
              }}
            >
              {t('confirmAdd')}
            </button>
          </div>
        </div>
      )}

      {mp?.installed && (hInfo?.magpie?.connected || provs.length > 0) && (
        <div id="ubConnectBox" className="note">
          <div className="row-between row-wrap">
            <span id="ubConnectText">
              {hInfo?.magpie?.connected
                ? t('ubConnectedMagpie', { label: hLabel, model: hInfo.magpie.model || t('defaultTag') })
                : t('ubNativeCliUsing', { label: hLabel })}
            </span>
            {!hInfo?.magpie?.connected && (
              <button
                type="button"
                id="ubConnectMagpie"
                className="mp go"
                disabled={busy}
                onClick={async () => {
                  onError('');
                  onBusyChange(true);
                  try {
                    const list = await onLoadModels(selectedHarness);
                    const model = defaultModel.trim() || list[0] || '';
                    if (!model) {
                      onError(t('ubPickModelFirst'));
                      return;
                    }
                    const updated = await api<SettingsData>(
                      `/api/harnesses/${encodeURIComponent(selectedHarness)}/magpie`,
                      { method: 'POST', body: { action: 'connect', model } }
                    );
                    onSettingsUpdated(updated);
                    if (!defaultModel.trim()) onChangeDefaultModel(model);
                    onResetModels();
                    onToast(t('ubConnectedToast', { label: hLabel }));
                  } catch (e) {
                    if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
                  } finally {
                    onBusyChange(false);
                  }
                }}
              >
                {t('ubConnectMagpieBtn')}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
};
