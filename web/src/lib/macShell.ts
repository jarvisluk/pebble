declare global {
  interface Window {
    __bothubMacShell?: boolean;
    _wails?: unknown;
    webkit?: {
      messageHandlers?: {
        external?: { postMessage: (msg: string) => void };
        bothubDrag?: { postMessage: (msg: string) => void };
      };
    };
  }
}

export function installMacWindowDragBridge(): () => void {
  let dragArmed = false;
  const inBar = (t: EventTarget | null) =>
    t instanceof Element &&
    Boolean(t.closest('.side-hd, .chat-hd, .set-hd')) &&
    !t.closest('button, input, select, textarea, a, label');

  const onMouseDown = (e: MouseEvent) => {
    dragArmed = e.button === 0 && e.detail === 1 && inBar(e.target);
  };
  const onMouseMove = (e: MouseEvent) => {
    if (!dragArmed || !(e.buttons & 1)) {
      dragArmed = false;
      return;
    }
    dragArmed = false;
    if (window.webkit?.messageHandlers?.external) {
      window.webkit.messageHandlers.external.postMessage('wails:drag');
    } else if (window.webkit?.messageHandlers?.bothubDrag) {
      window.webkit.messageHandlers.bothubDrag.postMessage('drag');
    }
  };
  const onMouseUp = () => {
    dragArmed = false;
  };
  const onBlur = () => {
    dragArmed = false;
  };
  const onDblClick = (e: MouseEvent) => {
    if (!inBar(e.target)) return;
    if (window.webkit?.messageHandlers?.external) {
      e.preventDefault();
      window.webkit.messageHandlers.external.postMessage('wails:drag:doubleclick');
    }
  };

  document.addEventListener('mousedown', onMouseDown);
  document.addEventListener('mousemove', onMouseMove);
  document.addEventListener('mouseup', onMouseUp);
  window.addEventListener('blur', onBlur);
  document.addEventListener('dblclick', onDblClick);

  return () => {
    document.removeEventListener('mousedown', onMouseDown);
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', onMouseUp);
    window.removeEventListener('blur', onBlur);
    document.removeEventListener('dblclick', onDblClick);
  };
}
