import React, { useEffect, useRef } from 'react';

interface WailsWebKitBridge {
  messageHandlers?: {
    external?: {
      postMessage: (msg: string) => void;
    };
  };
}

function postWailsDrag(msg: string) {
  (window as unknown as { webkit?: WailsWebKitBridge }).webkit?.messageHandlers?.external?.postMessage(msg);
}

export const DragBar: React.FC = () => {
  const dragArmedRef = useRef(false);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragArmedRef.current || !(e.buttons & 1)) {
        dragArmedRef.current = false;
        return;
      }
      dragArmedRef.current = false;
      postWailsDrag('wails:drag');
    };
    const clearArm = () => {
      dragArmedRef.current = false;
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', clearArm);
    window.addEventListener('blur', clearArm);
    return () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', clearArm);
      window.removeEventListener('blur', clearArm);
    };
  }, []);

  return (
    <div
      className="dsetup-drag"
      onMouseDown={(e) => {
        dragArmedRef.current = e.button === 0 && e.detail === 1;
      }}
      onDoubleClick={(e) => {
        const bridge = (window as unknown as { webkit?: WailsWebKitBridge }).webkit;
        if (bridge?.messageHandlers?.external) {
          e.preventDefault();
          postWailsDrag('wails:drag:doubleclick');
        }
      }}
    />
  );
};
