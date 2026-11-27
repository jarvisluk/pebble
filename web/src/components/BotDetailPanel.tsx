import React, { useEffect, useState } from 'react';
import type {
  BotDetail,
  BotStatus,
  BotTask,
  ConfirmOptions,
  DesktopTarget,
  NodeInfo,
  PermissionMode,
  ResolvedLang,
  SettingsData,
} from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';
import { botColor, contrastText } from '../lib/theme';
import { BotAppearancePicker } from './BotAppearancePicker';
import { BotAvatar } from './BotAvatar';
import { BotMemorySection } from './bot-detail/BotMemorySection';
import { BotRoutinesSection } from './bot-detail/BotRoutinesSection';
import { BotTasksSection } from './bot-detail/BotTasksSection';
import { BotTechDetailsSection } from './bot-detail/BotTechDetailsSection';

interface BotDetailPanelProps {
  open: boolean;
  detail: BotDetail | null;
  bots: BotStatus[];
  tasks: BotTask[];
  nodes: NodeInfo[];
  settings: SettingsData | null;
  models: Record<string, string[]>;
  routineRefreshSeq: number;
  lang: ResolvedLang;
  canBindDesktop?: boolean;
  canIsolateBrowser?: boolean;
  onClose: () => void;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onBotUpdated: (st: BotStatus) => void;
  onBotDeleted: (id: string) => void;
  onReloadBots: () => Promise<void>;
  onLoadModels: (harness: string, force?: boolean) => Promise<unknown>;
  onRefreshDetail: (id: string) => Promise<void>;
  onTasksLoaded: (botId: string, tasks: BotTask[]) => void;
  onInspectTask: (taskId: string) => void;
  onSettingsUpdated?: (s: SettingsData) => void;
  onOpenFiles?: (botId: string) => void;
}

export const BotDetailPanel: React.FC<BotDetailPanelProps> = ({
  open,
  detail,
  bots,
  tasks,
  nodes,
  settings,
  models,
  routineRefreshSeq,
  lang,
  canBindDesktop = true,
  canIsolateBrowser = true,
  onClose,
  onToast,
  onConfirm,
  onBotUpdated,
  onBotDeleted,
  onReloadBots,
  onLoadModels,
  onRefreshDetail,
  onTasksLoaded,
  onInspectTask,
  onSettingsUpdated,
  onOpenFiles,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  // Inline edit form fields
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editAvatar, setEditAvatar] = useState('');
  const [editColor, setEditColor] = useState('');
  const [appearanceOpen, setAppearanceOpen] = useState(false);
  const [editPrivate, setEditPrivate] = useState(false);
  const [editHarness, setEditHarness] = useState('opencode');
  const [editModel, setEditModel] = useState('');
  const [editDesktopTarget, setEditDesktopTarget] = useState('');
  const [editBrowserMode, setEditBrowserMode] = useState('');
  const [editPermissionMode, setEditPermissionMode] = useState<PermissionMode>('auto');
  const [desktopTargets, setDesktopTargets] = useState<DesktopTarget[]>([]);
  const [editInst, setEditInst] = useState('');
  const [editErr, setEditErr] = useState('');
  const [editSaving, setEditSaving] = useState(false);

  const loadDesktopTargets = async () => {
    try {
      const d = await api<{ targets?: DesktopTarget[] }>('/api/desktop/targets');
      setDesktopTargets(d.targets || []);
    } catch {}
  };

  useEffect(() => {
    if (!open || !detail) return;
    setAppearanceOpen(false);
    void loadDesktopTargets();
    if (detail.harness) void onLoadModels(detail.harness);
  }, [open, detail?.id]);


  useEffect(() => {
    if (!open || !detail) return;
    setEditName(detail.name || '');
    setEditDesc(detail.description || '');
    setEditAvatar(detail.avatar || '');
    setEditColor(detail.color || '');
    setEditPrivate(Boolean(detail.privateSessions));
    setEditHarness(detail.harness || settings?.defaultHarness || 'opencode');
    setEditModel((detail.modelInherited ? '' : detail.model || '').trim());
    setEditDesktopTarget(detail.desktopTarget || '');
    setEditBrowserMode(detail.browserMode || '');
    setEditPermissionMode((detail.permissionMode as PermissionMode) || 'auto');
    setEditInst(detail.instructions || '');
    setEditErr('');
  }, [
    open,
    detail?.id,
    detail?.name,
    detail?.description,
    detail?.avatar,
    detail?.color,
    detail?.privateSessions,
    detail?.harness,
    detail?.model,
    detail?.modelInherited,
    detail?.desktopTarget,
    detail?.browserMode,
    detail?.permissionMode,
    detail?.instructions,
  ]);

  const botAccentStyle: React.CSSProperties | undefined = detail
    ? ({
        '--bot-accent': botColor(detail.id, bots, editColor),
        '--bot-accent-text': contrastText(botColor(detail.id, bots, editColor)),
      } as React.CSSProperties)
    : undefined;

  const harnessLabel = (id?: string) =>
    settings?.harnesses.find((h) => h.id === id)?.label || id || 'opencode';

  const resolveBotDefaultModel = (g: string, h: string) => {
    if (!g) return '';
    if (!g.includes('/')) return g;
    return h === '' || h === 'opencode' || h === 'pi' ? g : '';
  };

  const origModel = detail ? (detail.modelInherited ? '' : detail.model || '').trim() : '';
  const origHarness = detail?.harness || settings?.defaultHarness || 'opencode';

  const isDirty = Boolean(
    detail &&
      (editName !== (detail.name || '') ||
        editDesc !== (detail.description || '') ||
        editAvatar !== (detail.avatar || '') ||
        editColor !== (detail.color || '') ||
        editPrivate !== Boolean(detail.privateSessions) ||
        editHarness !== origHarness ||
        editModel.trim() !== origModel ||
        (canBindDesktop && editDesktopTarget !== (detail.desktopTarget || '')) ||
        (canIsolateBrowser && editBrowserMode !== (detail.browserMode || '')) ||
        editPermissionMode !== ((detail.permissionMode as PermissionMode) || 'auto') ||
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
      const body: Record<string, unknown> = {
        name,
        description: editDesc.trim(),
        avatar: editAvatar,
        color: editColor,
        harness: editHarness,
        model: editModel.trim(),
        instructions: editInst,
        privateSessions: editPrivate,
      };
      if (canBindDesktop) body.desktopTarget = editDesktopTarget;
      if (canIsolateBrowser) body.browserMode = editBrowserMode;
      body.permissionMode = editPermissionMode;
      const st = await api<BotStatus>(`/api/bots/${encodeURIComponent(detail.id)}`, {
        method: 'PATCH',
        body,
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

            <BotMemorySection
              open={open}
              botId={detail.id}
              lang={lang}
              onToast={onToast}
            />

            <BotTasksSection
              open={open}
              detail={detail}
              tasks={tasks}
              nodes={nodes}
              lang={lang}
              onTasksLoaded={onTasksLoaded}
              onInspectTask={onInspectTask}
            />

            <BotRoutinesSection
              open={open}
              botId={detail.id}
              routineRefreshSeq={routineRefreshSeq}
              lang={lang}
              onToast={onToast}
              onConfirm={onConfirm}
            />

            <BotTechDetailsSection
              open={open}
              detail={detail}
              settings={settings}
              models={models}
              desktopTargets={desktopTargets}
              editHarness={editHarness}
              editModel={editModel}
              editDesktopTarget={editDesktopTarget}
              editBrowserMode={editBrowserMode}
              editPermissionMode={editPermissionMode}
              editPrivate={editPrivate}
              editInst={editInst}
              canBindDesktop={canBindDesktop}
              canIsolateBrowser={canIsolateBrowser}
              lang={lang}
              onChangeHarness={setEditHarness}
              onChangeModel={setEditModel}
              onChangeDesktopTarget={setEditDesktopTarget}
              onChangeBrowserMode={setEditBrowserMode}
              onChangePermissionMode={setEditPermissionMode}
              onChangePrivate={setEditPrivate}
              onChangeInst={setEditInst}
              onLoadModels={onLoadModels}
              onOpenFiles={onOpenFiles}
            />
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
