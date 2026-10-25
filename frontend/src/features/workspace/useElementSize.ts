import { useCallback, useEffect, useState } from 'react';

/**
 * The content box of the referenced element, kept current while it is
 * mounted. The element may appear late — the ref stays null while a render
 * takes an early return — so the observer attaches whenever one shows up
 * instead of only on the component's first mount.
 */
export function useElementSize<T extends HTMLElement>(): [(node: T | null) => void, { width: number; height: number }] {
  const [target, setTarget] = useState<T | null>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const ref = useCallback((node: T | null) => setTarget(node), []);
  useEffect(() => {
    if (!target) return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      setSize((current) => (current.width === width && current.height === height ? current : { width, height }));
    });
    observer.observe(target);
    return () => observer.disconnect();
  }, [target]);
  return [ref, size];
}
