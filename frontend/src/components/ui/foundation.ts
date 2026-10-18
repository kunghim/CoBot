// The shared foundation of CoBot's components: class joining and the scales
// every component draws from. The gallery (src/gallery) documents them; a
// value used outside these scales needs a reason.

/** Joins class names, skipping empty ones. */
export function cx(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ');
}

/** Control heights, shared by buttons, inputs and selects so rows align. */
export const controlHeight = {
  sm: 'h-7 text-xs',
  md: 'h-9 text-sm',
  lg: 'h-11 text-base',
} as const;
export type ControlSize = keyof typeof controlHeight;

/**
 * Hover and press feedback is a translucent layer of ink, never a fixed gray:
 * it shows on any surface (page, card, panel) in every theme and mode.
 */
export const interactive = {
  hover: 'hover:bg-neutral-900/[0.06] dark:hover:bg-white/[0.07]',
  pressed: 'active:bg-neutral-900/[0.1] dark:active:bg-white/[0.12]',
  selected: 'bg-neutral-900/[0.08] text-neutral-900 dark:bg-white/[0.1] dark:text-neutral-50',
} as const;

/**
 * The same layers as fixed classes, to show a state without the pointer
 * (state matrices in the gallery). Not for live components.
 */
export const shownState = {
  hover: 'bg-neutral-900/[0.06] dark:bg-white/[0.07]',
  pressed: 'bg-neutral-900/[0.1] dark:bg-white/[0.12]',
  focus: 'outline outline-2 outline-offset-2 outline-signal-500',
} as const;

/** Layers, lowest first. Anything that floats uses one of these. */
export const layer = {
  sticky: 'z-10',
  dropdown: 'z-40',
  sheet: 'z-40',
  dialog: 'z-50',
  toast: 'z-[60]',
  tooltip: 'z-[70]',
} as const;

/** Surfaces, from the page up. */
export const surface = {
  page: 'bg-neutral-50 dark:bg-neutral-950',
  sunken: 'bg-neutral-100 dark:bg-neutral-900',
  raised: 'bg-white dark:bg-neutral-900',
  overlay: 'bg-white shadow-xl ring-1 ring-neutral-900/10 dark:bg-neutral-800 dark:ring-white/10',
} as const;

/** Tones shared by alerts, badges, toasts and status dots. */
export type Tone = 'neutral' | 'info' | 'success' | 'warning' | 'danger';

export const toneSoft: Record<Tone, string> = {
  neutral: 'bg-neutral-900/[0.06] text-neutral-700 dark:bg-white/[0.08] dark:text-neutral-200',
  info: 'bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200',
  success: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200',
  warning: 'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200',
  danger: 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-200',
};

export const toneSolid: Record<Tone, string> = {
  neutral: 'bg-neutral-500',
  info: 'bg-sky-500',
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  danger: 'bg-red-500',
};

export const toneText: Record<Tone, string> = {
  neutral: 'text-neutral-600 dark:text-neutral-300',
  info: 'text-sky-700 dark:text-sky-300',
  success: 'text-emerald-700 dark:text-emerald-300',
  warning: 'text-amber-700 dark:text-amber-300',
  danger: 'text-red-600 dark:text-red-400',
};
