import { useEffect, useRef, useState } from 'react';

export type ViewState = 'active' | 'leaving' | 'hidden';
export type ViewDir = 'forward' | 'back';

export interface ViewTransitionResult<T extends string> {
  dir: ViewDir;
  leaving: T | null;
  animating: boolean;
  stateOf: (key: T) => ViewState;
}

/**
 * Coordinates simultaneous enter/leave view transitions while keeping panes mounted.
 */
export function useViewTransition<T extends string>(
  active: T,
  order: readonly T[],
  durationMs = 220
): ViewTransitionResult<T> {
  const prevRef = useRef<T>(active);
  const timerRef = useRef<number | null>(null);
  const [dir, setDir] = useState<ViewDir>('forward');
  const [leaving, setLeaving] = useState<T | null>(null);
  const [animating, setAnimating] = useState(false);

  useEffect(() => {
    const prev = prevRef.current;
    if (prev === active) return;
    prevRef.current = active;

    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    const prevIdx = order.indexOf(prev);
    const nextIdx = order.indexOf(active);
    const nextDir: ViewDir = nextIdx >= prevIdx ? 'forward' : 'back';
    setDir(nextDir);

    const reduced =
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    if (reduced) {
      setLeaving(null);
      setAnimating(false);
      return;
    }

    setLeaving(prev);
    setAnimating(true);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      setLeaving(null);
      setAnimating(false);
    }, durationMs);
  }, [active, durationMs, order]);

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
      }
    };
  }, []);

  const stateOf = (key: T): ViewState => {
    if (key === active) return 'active';
    if (key === leaving) return 'leaving';
    return 'hidden';
  };

  return { dir, leaving, animating, stateOf };
}
