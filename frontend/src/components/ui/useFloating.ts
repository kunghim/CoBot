import { useLayoutEffect, useState, type RefObject } from 'react';

export type Placement = 'bottom-start' | 'bottom-end' | 'top-start' | 'top-end';

export const placementClass: Record<Placement, string> = {
  'bottom-start': 'left-0 top-full mt-1.5',
  'bottom-end': 'right-0 top-full mt-1.5',
  'top-start': 'left-0 bottom-full mb-1.5',
  'top-end': 'right-0 bottom-full mb-1.5',
};

/**
 * Keeps a floating panel on screen: starting from the preferred placement,
 * it flips above when there is no room below and aligns to the other edge
 * when it would leave the window sideways.
 */
export function useFloatingPlacement(panel: RefObject<HTMLElement | null>, open: boolean, preferred: Placement): Placement {
  const [placement, setPlacement] = useState(preferred);
  useLayoutEffect(() => {
    if (!open) {
      setPlacement(preferred);
      return;
    }
    const element = panel.current;
    if (!element) return;
    const rect = element.getBoundingClientRect();
    let [vertical, horizontal] = preferred.split('-') as ['bottom' | 'top', 'start' | 'end'];
    if (vertical === 'bottom' && rect.bottom > window.innerHeight - 8 && rect.top - rect.height > 8) vertical = 'top';
    else if (vertical === 'top' && rect.top < 8) vertical = 'bottom';
    if (horizontal === 'start' && rect.right > window.innerWidth - 8) horizontal = 'end';
    else if (horizontal === 'end' && rect.left < 8) horizontal = 'start';
    const next = `${vertical}-${horizontal}` as Placement;
    if (next !== placement) setPlacement(next);
    // Measure once per opening; the panel does not move while open.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, preferred]);
  return placement;
}
