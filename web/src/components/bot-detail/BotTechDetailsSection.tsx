import React, { useEffect, useState } from 'react';
import type { BotDetail, DesktopTarget, PermissionMode, ResolvedLang, SettingsData } from '../../types';
import { translate } from '../../lib/i18n';

interface BotTechDetailsSectionProps {
  open: boolean;
  detail: BotDetail;
  settings: SettingsData | null;
  models: Record<string, string[]>;
  desktopTargets: DesktopTarget[];
  editHarness: string;
  editModel: string;
  editDesktopTarget: string;
  editBrowserMode: string;
  editPermissionMode: PermissionMode;
  editPrivate: boolean;
  editInst: string;
  canBindDesktop: boolean;
  canIsolateBrowser: boolean;
  lang: ResolvedLang;
  onChangeHarness: (v: string) => void;
  onChangeModel: (v: string) => void;
  onChangeDesktopTarget: (v: string) => void;
  onChangeBrowserMode: (v: string) => void;
  onChangePermissionMode: (v: PermissionMode) => void;
  onChangePrivate: (v: boolean) => void;
  onChangeInst: (v: string) => void;
  onLoadModels: (harness: string, force?: boolean) => Promise<unknown>;
  onOpenFiles?: (botId: string) => void;
}

export const BotTechDetailsSection: React.FC<BotTechDetailsSectionProps> = ({
  open,
  detail,
  settings,
  models,
  desktopTargets,
  editHarness,
  editModel,
  editDesktopTarget,
  editBrowserMode,
  editPermissionMode,
  editPrivate,
  editInst,
  canBindDesktop,
  canIsolateBrowser,
  lang,
  onChangeHarness,
  onChangeModel,
  onChangeDesktopTarget,
  onChangeBrowserMode,
  onChangePermissionMode,
  onChangePrivate,
  onChangeInst,
  onLoadModels,
  onOpenFiles,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [techOpen, setTechOpen] = useState(false);

  useEffect(() => {
    if (open && detail.id) {
      setTechOpen(false);
    }
  }, [open, detail.id]);

  const harnessLabel = (id?: string) =>
    settings?.harnesses.find((h) => h.id === id)?.label || id || 'opencode';

  const resolveBotDefaultModel = (g: string, h: string) => {
    if (!g) return '';
    if (!g.includes('/')) return g;
    return h === '' || h === 'opencode' || h === 'pi' ? g : '';
  };

  return (
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
            <dd id="pWs">
              {onOpenFiles ? (
                <button
                  type="button"
                  className="ws-link-btn"
                  onClick={() => onOpenFiles(detail.id)}
                  title={t('workspaceFiles')}
                >
                  <span>{detail.workspace}</span>
                </button>
              ) : (
                detail.workspace
              )}
            </dd>
          </div>
        </dl>
        <div className="stack6 mt-10">
          <div className="fld">
            <label htmlFor="peHarness">{t('statHarness')}</label>
            <select
              id="peHarness"
              value={editHarness}
              onChange={(e) => {
                const nextH = e.target.value;
                if (nextH === editHarness) return;
                onChangeHarness(nextH);
                onChangeModel('');
                void onLoadModels(nextH);
              }}
            >
              {(settings?.harnesses || [{ id: editHarness, label: harnessLabel(editHarness) }]).map((h) => (
                <option key={h.id} value={h.id}>
                  {h.label}
                </option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label htmlFor="peModel">{t('model')}</label>
            <input
              id="peModel"
              className="mono"
              list="bot-detail-models"
              spellCheck={false}
              placeholder={(() => {
                const per = settings?.defaultModels?.[editHarness]?.trim() || '';
                const def =
                  per || resolveBotDefaultModel((settings?.defaultModel || '').trim(), editHarness);
                return def ? t('defaultModelPh', { model: def }) : t('inheritGlobalPh');
              })()}
              value={editModel}
              onFocus={() => void onLoadModels(editHarness)}
              onChange={(e) => onChangeModel(e.target.value)}
            />
            <datalist id="bot-detail-models">
              {(models[editHarness] || []).map((m) => (
                <option key={m} value={m} />
              ))}
            </datalist>
          </div>
        </div>
        {(() => {
          const seenOpts = new Set<string>(['', 'local']);
          const screenOpts: Array<{ value: string; label: string }> = [
            { value: '', label: t('deskBindDefault') },
            { value: 'local', label: t('localDesktopName') },
          ];
          const localEntry = desktopTargets.find((tr) => tr.id === 'local');
          if (localEntry?.virtualSupported) {
            const vId = `virtual:${detail.id}`;
            seenOpts.add(vId);
            screenOpts.push({ value: vId, label: t('deskBindLocalVirtual') });
          }
          for (const tr of desktopTargets) {
            if (tr.id === 'local') continue;
            if (!seenOpts.has(tr.id)) {
              seenOpts.add(tr.id);
              screenOpts.push({ value: tr.id, label: tr.name || tr.id });
            }
            if (!tr.virtual && tr.virtualSupported) {
              const rvId = `${tr.id}:virtual:${detail.id}`;
              if (!seenOpts.has(rvId)) {
                seenOpts.add(rvId);
                screenOpts.push({
                  value: rvId,
                  label: t('deskBindRemoteVirtual', { name: tr.name || tr.id }),
                });
              }
            }
          }
          if (editDesktopTarget && !seenOpts.has(editDesktopTarget)) {
            screenOpts.push({
              value: editDesktopTarget,
              label: t('deskBindOfflineTag', { id: editDesktopTarget }),
            });
          }
          if (!canBindDesktop && !canIsolateBrowser) return null;
          return (
            <div className="stack6 mt-10">
              {canBindDesktop && (
                <div className="fld">
                  <label htmlFor="peDesktopTarget">{t('botDesktopTarget')}</label>
                  <select
                    id="peDesktopTarget"
                    value={editDesktopTarget}
                    onChange={(e) => onChangeDesktopTarget(e.target.value)}
                  >
                    {screenOpts.map((opt) => (
                      <option key={opt.value || '_default'} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>
              )}
              {canIsolateBrowser && (
                <div className="fld">
                  <label htmlFor="peBrowserMode">{t('botBrowserMode')}</label>
                  <select
                    id="peBrowserMode"
                    value={editBrowserMode}
                    onChange={(e) => onChangeBrowserMode(e.target.value)}
                  >
                    <option value="">{t('browserModeAuto')}</option>
                    <option value="isolated">{t('browserModeIsolated')}</option>
                    <option value="attach">{t('browserModeAttach')}</option>
                  </select>
                </div>
              )}
              {(() => {
                const hCaps = settings?.harnesses.find((h) => h.id === detail.harness)?.permissions?.modes;
                const supported: PermissionMode[] = hCaps && hCaps.length > 0 ? hCaps : ['auto', 'edit', 'ask', 'plan'];
                const activeVal = supported.includes(editPermissionMode) ? editPermissionMode : supported[0] || 'auto';
                return (
                  <div className="fld">
                    <label htmlFor="pePermissionMode">{t('permMode')}</label>
                    <select
                      id="pePermissionMode"
                      disabled={supported.length <= 1}
                      value={activeVal}
                      onChange={(e) => onChangePermissionMode(e.target.value as PermissionMode)}
                    >
                      {supported.map((m) => (
                        <option key={m} value={m}>
                          {t(`permMode_${m}`)}
                        </option>
                      ))}
                    </select>
                    <div className="set-card-desc mt-4">
                      {supported.length <= 1
                        ? t('permModeUnsupported', { mode: t(`permMode_${supported[0] || 'auto'}`) })
                        : t(`permDesc_${activeVal}`)}
                    </div>
                  </div>
                );
              })()}
            </div>
          );
        })()}
        <label className="perm self-start mt-10">
          <input
            type="checkbox"
            id="pePrivate"
            checked={editPrivate}
            onChange={(e) => onChangePrivate(e.target.checked)}
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
          onChange={(e) => onChangeInst(e.target.value)}
        />
      </div>
    </details>
  );
};
