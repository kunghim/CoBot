/** The frame every text-like control shares: border, focus ring. The pill
 * variant keeps the same chrome at full radius for toolbar-scale controls
 * (the header search) that sit beside capsule controls. */
const frameChrome =
  'border border-neutral-300 bg-white text-neutral-900 shadow-sm transition-colors placeholder:text-neutral-400 hover:border-neutral-400 focus-within:border-signal-500 focus-within:ring-2 focus-within:ring-signal-500/25 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100 dark:hover:border-neutral-600';
export const controlFrame = `rounded-lg ${frameChrome}`;
export const controlFramePill = `rounded-full ${frameChrome}`;
