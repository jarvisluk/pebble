import { useCallback, useEffect } from 'react';
import type React from 'react';
import type { ConfirmOptions, SettingsData, SettingsTab } from '../types';

export interface UseAppLayoutOpts {
  sideWidth: number | null;
  confirmOpts: ConfirmOptions | null;
  unboxOpen: boolean;
  createOpen: boolean;
  panelOpen: boolean;
  collabPeerId: string | null;
  inspectedTaskId: string | null;
  mobileSideOpen: boolean;
  viewRef: React.MutableRefObject<'chat' | 'settings'>;
  filterInputRef: React.RefObject<HTMLInputElement | null>;
  closeAllOverlays: () => void;
  leaveSettings: () => void;
  loadSettings: (fresh?: boolean) => Promise<SettingsData>;
  setSideCollapsed: React.Dispatch<React.SetStateAction<boolean>>;
  setMobileSideOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setConfirmOpts: React.Dispatch<React.SetStateAction<ConfirmOptions | null>>;
  setUnboxOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setDeskOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setSettingsTab: React.Dispatch<React.SetStateAction<SettingsTab>>;
  setView: React.Dispatch<React.SetStateAction<'chat' | 'settings'>>;
  setSideWidth: React.Dispatch<React.SetStateAction<number | null>>;
  setSideResizing: React.Dispatch<React.SetStateAction<boolean>>;
  setInspectedTaskId: React.Dispatch<React.SetStateAction<string | null>>;
}

export function useAppLayout(opts: UseAppLayoutOpts) {
  const {
    sideWidth,
    confirmOpts,
    unboxOpen,
    createOpen,
    panelOpen,
    collabPeerId,
    inspectedTaskId,
    mobileSideOpen,
    viewRef,
    filterInputRef,
    closeAllOverlays,
    leaveSettings,
    loadSettings,
    setSideCollapsed,
    setMobileSideOpen,
    setConfirmOpts,
    setUnboxOpen,
    setDeskOpen,
    setSettingsTab,
    setView,
    setSideWidth,
    setSideResizing,
    setInspectedTaskId,
  } = opts;

  const toggleDesktopSidebar = useCallback((forceCollapsed: boolean | null = null) => {
    setSideCollapsed((prev) => {
      const next = forceCollapsed !== null ? forceCollapsed : !prev;
      try {
        if (next) localStorage.setItem('bothub.sideCollapsed', '1');
        else localStorage.removeItem('bothub.sideCollapsed');
      } catch {}
      return next;
    });
  }, []);

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 820px)');
    const onMq = (e: MediaQueryListEvent) => {
      if (!e.matches) setMobileSideOpen(false);
    };
    mq.addEventListener('change', onMq);

    const onKeyDown = (e: KeyboardEvent) => {
      if (document.activeElement?.hasAttribute('data-desk-stage')) return;
      if (e.key === 'Escape') {
        if (confirmOpts) setConfirmOpts(null);
        else if (unboxOpen) setUnboxOpen(false);
        else if (inspectedTaskId) setInspectedTaskId(null);
        else if (createOpen || panelOpen || collabPeerId || mobileSideOpen) closeAllOverlays();
        else if (viewRef.current === 'settings') leaveSettings();
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        if (viewRef.current !== 'settings') filterInputRef.current?.focus();
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b' && matchMedia('(min-width: 821px)').matches) {
        e.preventDefault();
        if (viewRef.current !== 'settings') toggleDesktopSidebar();
      }
      if ((e.metaKey || e.ctrlKey) && e.key === ',') {
        e.preventDefault();
        if (viewRef.current === 'settings') leaveSettings();
        else {
          closeAllOverlays();
          setDeskOpen(false);
          setSettingsTab('general');
          setView('settings');
          void loadSettings();
        }
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      mq.removeEventListener('change', onMq);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [closeAllOverlays, collabPeerId, confirmOpts, createOpen, inspectedTaskId, leaveSettings, loadSettings, mobileSideOpen, panelOpen, toggleDesktopSidebar, unboxOpen]);

  const handleSideResizeStart = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.button !== 0) return;
      e.preventDefault();
      const sideEl = e.currentTarget.previousElementSibling as HTMLElement | null;
      const startWidth = sideWidth ?? (sideEl ? Math.round(sideEl.getBoundingClientRect().width) : 300);
      const startX = e.clientX;
      let latest = startWidth;

      setSideResizing(true);
      document.body.classList.add('resizing-side');

      const onMove = (ev: MouseEvent) => {
        const maxW = Math.min(480, Math.floor(window.innerWidth * 0.45));
        const next = Math.min(maxW, Math.max(220, Math.round(startWidth + (ev.clientX - startX))));
        latest = next;
        setSideWidth(next);
      };
      const onUp = () => {
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
        document.body.classList.remove('resizing-side');
        setSideResizing(false);
        try {
          localStorage.setItem('bothub.sideWidth', String(latest));
        } catch {}
      };
      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
    },
    [sideWidth]
  );

  const handleSideResizeReset = useCallback(() => {
    setSideWidth(null);
    try {
      localStorage.removeItem('bothub.sideWidth');
    } catch {}
  }, []);


  return {
    toggleDesktopSidebar,
    handleSideResizeStart,
    handleSideResizeReset,
  };
}
