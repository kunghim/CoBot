// Choosing the model and how hard it thinks, as Codex does: a compact
// trigger in the composer opens a panel with the current effort (in the
// signal color) and model on top, a stepped slider for the effort, a way to
// switch the model, and a reset to the default.
import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react';
import { Brain, Check, ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react';
import { IconButton } from './Button';
import { cx, interactive, layer, surface } from './foundation';
import { useDismiss } from './useDismiss';
import { placementClass, useFloatingPlacement, type Placement } from './useFloating';

export interface EffortLevel {
  value: string;
  label: string;
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
      <div className="absolute inset-y-0 left-0 rounded-full bg-signal-500 transition-[width] duration-150" style={{ width: `calc(32px + (100% - 32px) * ${at(index) / 100})` }} />
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
          className="absolute top-1/2 h-7 w-7 -translate-x-1/2 -translate-y-1/2 rounded-full bg-on-signal shadow-md ring-1 ring-neutral-900/10 transition-[left] duration-150"
          style={{ left: `${at(index)}%` }}
        />
      </div>
    </div>
  );
}

export interface ModelOption {
  id: string;
  label: string;
  description?: ReactNode;
}

/** The panel's content: effort and model on top, then the slider or the model list. */
export function ModelEffortPanel({ models, model, onModel, levels, effort, onEffort, isDefault, onReset, labels }: { models: ModelOption[]; model: string; onModel: (id: string) => void; levels: EffortLevel[]; effort: string; onEffort: (value: string) => void; isDefault: boolean; onReset: () => void; labels: { effort: string; reset: string; chooseModel: string; back: string } }) {
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
        <ul role="listbox" aria-label={labels.chooseModel} className="mt-2 space-y-0.5">
          {models.map((entry) => (
            <li key={entry.id} role="option" aria-selected={entry.id === model}>
              <button
                type="button"
                onClick={() => {
                  onModel(entry.id);
                  setChoosing(false);
                }}
                className={cx('flex w-full items-start gap-2 rounded-lg px-2 py-1.5 text-left text-sm', interactive.hover)}
              >
                <Check size={15} aria-hidden className={cx('mt-0.5 shrink-0', entry.id === model ? 'text-signal-500' : 'opacity-0')} />
                <span className="min-w-0">
                  <span className="block font-medium text-neutral-900 dark:text-neutral-50">{entry.label}</span>
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
        <Brain size={16} aria-hidden className="mt-1 shrink-0 text-neutral-400" />
        <div className="min-w-0 flex-1 text-center">
          <p className="text-sm font-semibold text-signal-600 dark:text-signal-400" aria-live="polite">{level?.label}</p>
          <button type="button" onClick={() => setChoosing(true)} className="mt-0.5 inline-flex max-w-full items-center gap-0.5 rounded-md px-1.5 text-sm font-medium text-neutral-800 hover:bg-neutral-900/[0.06] dark:text-neutral-100 dark:hover:bg-white/[0.08]">
            <span className="truncate">{current?.label ?? model}</span>
            <ChevronRight size={14} aria-hidden className="shrink-0 text-neutral-400" />
          </button>
        </div>
        <IconButton icon={RotateCcw} label={labels.reset} size="sm" shape="circle" disabled={isDefault} onClick={onReset} />
      </div>
      <div className="mt-3">
        <SteppedSlider levels={levels} value={effort} onChange={onEffort} label={labels.effort} />
      </div>
    </div>
  );
}

/** The composer's trigger and the floating panel it opens. */
export function ModelEffortPicker({ placement = 'top-start', triggerLabel, disabled = false, note, ...panel }: Parameters<typeof ModelEffortPanel>[0] & { placement?: Placement; triggerLabel: string; disabled?: boolean; note?: ReactNode }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const floating = useRef<HTMLDivElement>(null);
  const placed = useFloatingPlacement(floating, open, placement);
  useDismiss(root, open, () => setOpen(false));
  const level = panel.levels.find((entry) => entry.value === panel.effort);
  const model = panel.models.find((entry) => entry.id === panel.model);
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
        <Brain size={14} aria-hidden />
        <span className="text-signal-600 dark:text-signal-400">{level?.label}</span>
        <span className="max-w-[9rem] truncate">{model?.label}</span>
      </button>
      {open && (
        <div ref={floating} role="dialog" aria-label={triggerLabel} className={cx('absolute w-72 animate-pop-in rounded-2xl p-3', surface.overlay, layer.dropdown, placementClass[placed])}>
          <ModelEffortPanel {...panel} />
          {note && <p className="mt-3 text-[11px] leading-4 text-neutral-500 dark:text-neutral-400">{note}</p>}
        </div>
      )}
    </div>
  );
}
