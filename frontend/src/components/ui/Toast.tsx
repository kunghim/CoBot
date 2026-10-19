// Toast: a short note that something happened, away from where it happened
// ("Saved", "Copied"), dropping in at the top center of the window. Success leaves by itself; a failure stays until it is
// closed and says what to do. Errors about a field go under the field, not
// in a toast. Screen readers hear every toast.
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from 'lucide-react';
import { cx, layer, type Tone } from './foundation';

export interface ToastInput {
  tone?: Exclude<Tone, 'neutral'>;
  title: ReactNode;
  description?: ReactNode;
  /** An action the note offers, e.g. Undo. */
  action?: { label: string; onClick: () => void };
  /** Milliseconds; failures default to staying until closed. */
  duration?: number;
}

interface ToastEntry extends ToastInput {
  id: number;
}

interface ToastApi {
  show: (toast: ToastInput) => number;
  dismiss: (id: number) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

/** Shows toasts from anywhere under ToastProvider. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (!api) throw new Error('useToast must be used inside ToastProvider');
  return api;
}

const icons = { info: Info, success: CircleCheck, warning: TriangleAlert, danger: CircleAlert };
const iconTone = { info: 'text-sky-500', success: 'text-emerald-500', warning: 'text-amber-500', danger: 'text-red-500' };

export function ToastProvider({ children, closeLabel }: { children: ReactNode; closeLabel: string }) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const next = useRef(1);
  const dismiss = useCallback((id: number) => setToasts((list) => list.filter((toast) => toast.id !== id)), []);
  const show = useCallback((toast: ToastInput) => {
    const id = next.current++;
    // At most three at a time; the oldest gives way.
    setToasts((list) => [...list.slice(-2), { ...toast, id }]);
    return id;
  }, []);
  const api = useMemo(() => ({ show, dismiss }), [show, dismiss]);
  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastRegion toasts={toasts} dismiss={dismiss} closeLabel={closeLabel} />
    </ToastContext.Provider>
  );
}

function ToastRegion({ toasts, dismiss, closeLabel }: { toasts: ToastEntry[]; dismiss: (id: number) => void; closeLabel: string }) {
  return (
    <div className={cx('pointer-events-none fixed left-1/2 top-4 flex w-[24rem] max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-col gap-2', layer.toast)}>
      <div aria-live="polite" role="status" className="flex flex-col gap-2">
        {toasts.filter((toast) => toast.tone !== 'danger').map((toast) => (
          <ToastCard key={toast.id} toast={toast} dismiss={dismiss} closeLabel={closeLabel} />
        ))}
      </div>
      <div aria-live="assertive" role="alert" className="flex flex-col gap-2">
        {toasts.filter((toast) => toast.tone === 'danger').map((toast) => (
          <ToastCard key={toast.id} toast={toast} dismiss={dismiss} closeLabel={closeLabel} />
        ))}
      </div>
    </div>
  );
}

export function ToastCard({ toast, dismiss, closeLabel }: { toast: ToastEntry; dismiss: (id: number) => void; closeLabel: string }) {
  const tone = toast.tone ?? 'success';
  const Icon = icons[tone];
  const duration = toast.duration ?? (tone === 'danger' ? 0 : 2500);
  const [paused, setPaused] = useState(false);
  useEffect(() => {
    if (!duration || paused) return;
    const timer = window.setTimeout(() => dismiss(toast.id), duration);
    return () => window.clearTimeout(timer);
  }, [duration, paused, dismiss, toast.id]);
  return (
    <div
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      className="pointer-events-auto flex animate-drop-in items-start gap-3 rounded-xl bg-card p-3.5 shadow-xl ring-1 ring-neutral-900/10 dark:ring-white/10"
    >
      <Icon size={18} aria-hidden className={cx('mt-px shrink-0', iconTone[tone])} />
      <div className="min-w-0 flex-1 text-sm">
        <p className="font-medium text-neutral-900 dark:text-neutral-50">{toast.title}</p>
        {toast.description && <p className="mt-0.5 leading-5 text-neutral-600 dark:text-neutral-300">{toast.description}</p>}
        {toast.action && (
          <button
            type="button"
            onClick={() => {
              toast.action!.onClick();
              dismiss(toast.id);
            }}
            className="mt-2 text-sm font-medium text-signal-600 hover:underline dark:text-signal-400"
          >
            {toast.action.label}
          </button>
        )}
      </div>
      <button type="button" onClick={() => dismiss(toast.id)} aria-label={closeLabel} title={closeLabel} className="-m-1 rounded-md p-1 text-neutral-400 hover:bg-neutral-900/[0.06] hover:text-neutral-700 dark:hover:bg-white/[0.08] dark:hover:text-neutral-200">
        <X size={14} aria-hidden />
      </button>
    </div>
  );
}
