// Choosing the model and how hard it thinks, as Codex does: a compact
// trigger in the composer opens a panel with the current effort (in the
// signal color) and model on top, a stepped slider for the effort, a way to
// switch the model, and a reset to the default.
import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react';
import { Brain, Check, ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react';
import { IconButton } from './Button';
import { Switch } from './Choice';
import { Badge } from './Feedback';
import { cx, interactive, layer, surface } from './foundation';
import { useDismiss } from './useDismiss';
import { placementClass, useFloatingPlacement, type Placement } from './useFloating';

export interface EffortLevel {
  value: string;
  label: string;
}

/** The specks that stream along the track at the highest level: height from the top, size, speed and when each starts. */
const SPARKS = [
  { y: '18%', size: 3, duration: 1.7, delay: 0 },
  { y: '70%', size: 2, duration: 2.1, delay: 0.35 },
  { y: '42%', size: 4, duration: 1.4, delay: 0.7 },
  { y: '82%', size: 3, duration: 1.9, delay: 1.05 },
  { y: '28%', size: 2, duration: 2.3, delay: 1.4 },
  { y: '58%', size: 3, duration: 1.6, delay: 0.2 },
  { y: '12%', size: 2, duration: 2.0, delay: 0.9 },
  { y: '50%', size: 3, duration: 1.5, delay: 1.2 },
];

/** Whether a level is the highest of several: the one that gets the show. */
function isTop(levels: EffortLevel[], value: string): boolean {
  return levels.length > 1 && levels[levels.length - 1].value === value;
}

/**
 * A slider that snaps to a few named steps. Drag, click a step, or use the
 * arrow keys; screen readers hear the step's name.
 */
export function SteppedSlider({ levels, value, onChange, label }: { levels: EffortLevel[]; value: string; onChange: (value: string) => void; label: string }) {
  const track = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);
  const index = Math.max(0, levels.findIndex((level) => level.value === value));
  const last = levels.length - 1;
  const at = (i: number) => (last === 0 ? 0 : (i / last) * 100);
  const top = isTop(levels, value);
  const pick = (clientX: number) => {
    const rect = track.current?.getBoundingClientRect();
    if (!rect) return;
    const ratio = Math.min(1, Math.max(0, (clientX - rect.left - 16) / (rect.width - 32)));
    const next = Math.round(ratio * last);
    if (next !== index) onChange(levels[next].value);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    let next = index;
    if (event.key === 'ArrowRight' || event.key === 'ArrowUp') next = Math.min(last, index + 1);
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') next = Math.max(0, index - 1);
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = last;
    else return;
    event.preventDefault();
    if (next !== index) onChange(levels[next].value);
  };
  return (
    <div
      ref={track}
      onPointerDown={(event: PointerEvent) => {
        dragging.current = true;
        (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
        pick(event.clientX);
      }}
      onPointerMove={(event: PointerEvent) => dragging.current && pick(event.clientX)}
      onPointerUp={() => (dragging.current = false)}
      className="relative h-8 cursor-pointer touch-none select-none rounded-full bg-neutral-900/[0.07] dark:bg-white/[0.08]"
    >
      {/* Filled part, from the start to the knob. */}
      <div className={cx('absolute inset-y-0 left-0 rounded-full transition-[width] duration-150', top ? 'effort-max-fill' : 'bg-signal-500')} style={{ width: `calc(32px + (100% - 32px) * ${at(index) / 100})` }} />
      {top && (
        <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden rounded-full">
          {SPARKS.map((spark, i) => (
            <span key={i} className="effort-spark" style={{ '--y': spark.y, '--size': `${spark.size}px`, '--duration': `${spark.duration}s`, '--delay': `${spark.delay}s` } as React.CSSProperties} />
          ))}
        </div>
      )}
      {/* Step marks. */}
      <div className="absolute inset-x-4 inset-y-0">
        {levels.map((level, i) => (
          <span key={level.value} aria-hidden className={cx('absolute top-1/2 h-1.5 w-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full', i <= index ? 'bg-on-signal/70' : 'bg-neutral-900/25 dark:bg-white/25')} style={{ left: `${at(i)}%` }} />
        ))}
        <span
          role="slider"
          tabIndex={0}
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={last}
          aria-valuenow={index}
          aria-valuetext={levels[index]?.label}
          onKeyDown={onKeyDown}
          className={cx('absolute top-1/2 h-7 w-7 -translate-x-1/2 -translate-y-1/2 rounded-full bg-on-signal transition-[left] duration-150', top ? 'effort-max-knob' : 'shadow-md ring-1 ring-neutral-900/10')}
          style={{ left: `${at(index)}%` }}
        >
          {/* A new one each time the top is reached, so the ring leaves the knob once. */}
          {top && <span key={index} aria-hidden className="effort-burst" />}
        </span>
      </div>
    </div>
  );
}

export interface ModelOption {
  id: string;
  label: string;
  description?: ReactNode;
  /** The heading the model is listed under, such as its provider. */
  group?: string;
  /** Shown but not choosable, such as a model that failed its check. */
  disabled?: boolean;
  /** A short mark after the name, such as "1M" for a long context. */
  badge?: string;
}

/** A model whose thinking can be switched off: a switch in the panel. */
export interface ThinkingSwitch {
  on: boolean;
  onChange: (on: boolean) => void;
  label: string;
}

/** The panel's content: effort and model on top, then the slider or the model list. */
export function ModelEffortPanel({ models, model, onModel, levels, effort, onEffort, isDefault, onReset, thinking, labels }: { models: ModelOption[]; model: string; onModel: (id: string) => void; levels: EffortLevel[]; effort: string; onEffort: (value: string) => void; isDefault: boolean; onReset: () => void; thinking?: ThinkingSwitch; labels: { effort: string; reset: string; chooseModel: string; back: string } }) {
  const [choosing, setChoosing] = useState(false);
  const current = models.find((entry) => entry.id === model);
  const level = levels.find((entry) => entry.value === effort);
  if (choosing) {
    return (
      <div>
        <div className="flex items-center gap-1">
          <IconButton icon={ChevronLeft} label={labels.back} size="sm" shape="circle" onClick={() => setChoosing(false)} />
          <span className="text-sm font-semibold">{labels.chooseModel}</span>
        </div>
        <ul role="listbox" aria-label={labels.chooseModel} className="mt-2 max-h-80 space-y-0.5 overflow-y-auto">
          {models.map((entry, index) => (
            <li key={entry.id} role="option" aria-selected={entry.id === model} aria-disabled={entry.disabled || undefined}>
              {entry.group && entry.group !== models[index - 1]?.group && (
                <p role="presentation" className="px-2 pb-0.5 pt-2 text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 first:pt-0 dark:text-neutral-400">
                  {entry.group}
                </p>
              )}
              <button
                type="button"
                disabled={entry.disabled}
                onClick={() => {
                  onModel(entry.id);
                  setChoosing(false);
                }}
                className={cx('flex w-full items-start gap-2 rounded-lg px-2 py-1.5 text-left text-sm', interactive.hover, 'disabled:pointer-events-none disabled:opacity-50')}
              >
                <Check size={15} aria-hidden className={cx('mt-0.5 shrink-0', entry.id === model ? 'text-signal-500' : 'opacity-0')} />
                <span className="min-w-0">
                  <span className="flex items-center gap-1.5 font-medium text-neutral-900 dark:text-neutral-50">
                    {entry.label}
                    {entry.badge && <Badge tone="success">{entry.badge}</Badge>}
                  </span>
                  {entry.description && <span className="block text-xs text-neutral-500 dark:text-neutral-400">{entry.description}</span>}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    );
  }
  return (
    <div>
      <div className="flex items-start">
        {levels.length > 0 || thinking ? <Brain size={16} aria-hidden className="mt-1 shrink-0 text-neutral-400" /> : <span aria-hidden className="w-4 shrink-0" />}
        <div className="min-w-0 flex-1 text-center">
          <p className={cx('text-sm font-semibold', !thinking?.on === true && isTop(levels, effort) ? 'effort-max-text' : 'text-signal-600 dark:text-signal-400')} aria-live="polite">{thinking && !thinking.on ? thinking.label : level?.label}</p>
          <button type="button" onClick={() => setChoosing(true)} className="mt-0.5 inline-flex max-w-full items-center gap-0.5 rounded-md px-1.5 text-sm font-medium text-neutral-800 hover:bg-neutral-900/[0.06] dark:text-neutral-100 dark:hover:bg-white/[0.08]">
            <span className="truncate">{current?.label ?? model}</span>
            {current?.badge && <Badge tone="success">{current.badge}</Badge>}
            <ChevronRight size={14} aria-hidden className="shrink-0 text-neutral-400" />
          </button>
        </div>
        {levels.length > 0 || thinking ? <IconButton icon={RotateCcw} label={labels.reset} size="sm" shape="circle" disabled={isDefault} onClick={onReset} /> : <span aria-hidden className="w-8 shrink-0" />}
      </div>
      {thinking && (
        <div className="mt-3">
          <Switch checked={thinking.on} onChange={thinking.onChange} label={thinking.label} />
        </div>
      )}
      <div className={cx('mt-3', thinking && !thinking.on && 'pointer-events-none opacity-40')}>
        {levels.length > 0 && <SteppedSlider levels={levels} value={effort} onChange={onEffort} label={labels.effort} />}
      </div>
    </div>
  );
}

/** The composer's trigger and the floating panel it opens. The open state
 * can be controlled, so an entry elsewhere — a failed turn's switch-model
 * action — can open the same picker. */
export function ModelEffortPicker({ placement = 'top-start', triggerLabel, disabled = false, note, open: openProp, onOpenChange, ...panel }: Parameters<typeof ModelEffortPanel>[0] & { placement?: Placement; triggerLabel: string; disabled?: boolean; note?: ReactNode; open?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [openState, setOpenState] = useState(false);
  const open = openProp ?? openState;
  const setOpen = (value: boolean) => {
    setOpenState(value);
    onOpenChange?.(value);
  };
  const root = useRef<HTMLDivElement>(null);
  const floating = useRef<HTMLDivElement>(null);
  const placed = useFloatingPlacement(floating, open, placement);
  useDismiss(root, open, () => setOpen(false));
  const level = panel.levels.find((entry) => entry.value === panel.effort);
  const model = panel.models.find((entry) => entry.id === panel.model);
  const off = panel.thinking !== undefined && !panel.thinking.on;
  return (
    <div ref={root} className="relative inline-flex">
      <button
        type="button"
        aria-expanded={open}
        aria-haspopup="dialog"
        disabled={disabled}
        aria-label={triggerLabel}
        title={triggerLabel}
        onClick={() => setOpen(!open)}
        className={cx('inline-flex h-7 items-center gap-1.5 rounded-lg px-2 text-xs font-medium text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100', interactive.hover, open && interactive.selected, 'disabled:pointer-events-none disabled:opacity-50')}
      >
        {(panel.levels.length > 0 || panel.thinking) && <Brain size={14} aria-hidden />}
        <span className={!off && isTop(panel.levels, panel.effort) ? 'effort-max-text' : 'text-signal-600 dark:text-signal-400'}>{off ? panel.thinking?.label : level?.label}</span>
        <span className="max-w-[9rem] truncate">{model?.label}</span>
        {model?.badge && <Badge tone="success">{model.badge}</Badge>}
      </button>
      {open && (
        <div ref={floating} role="dialog" aria-label={triggerLabel} className={cx('absolute w-72 animate-pop-in rounded-2xl p-3', surface.overlay, layer.dropdown, placementClass[placed])}>
          <ModelEffortPanel {...panel} />
          {note && <p className="mt-3 text-2xs leading-4 text-neutral-500 dark:text-neutral-400">{note}</p>}
        </div>
      )}
    </div>
  );
}
