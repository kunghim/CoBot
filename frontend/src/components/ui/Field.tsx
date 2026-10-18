// Form fields: a label, the control, a hint and an error, wired together for
// screen readers. Errors sit under the field they are about and say how to
// fix it; saving succeeded is announced by a toast, not next to the field.
import { createContext, useContext, useId, type ReactNode } from 'react';
import { CircleAlert } from 'lucide-react';
import { cx } from './foundation';

interface FieldContext {
  id: string;
  describedBy?: string;
  invalid: boolean;
}

const Context = createContext<FieldContext | null>(null);

/** The ids a control inside a Field should use. */
export function useField(): FieldContext | null {
  return useContext(Context);
}

export function Field({
  label,
  hint,
  error,
  required = false,
  children,
  className,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  required?: boolean;
  children: ReactNode;
  className?: string;
}) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [errorId, hintId].filter(Boolean).join(' ') || undefined;
  return (
    <Context.Provider value={{ id, describedBy, invalid: !!error }}>
      <div className={cx('space-y-1.5', className)}>
        <label htmlFor={id} className="block text-sm font-medium text-neutral-800 dark:text-neutral-100">
          {label}
          {required && <span className="ml-0.5 text-red-600" aria-hidden>*</span>}
        </label>
        {children}
        {hint && !error && <p id={hintId} className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{hint}</p>}
        {error && <FieldError id={errorId}>{error}</FieldError>}
      </div>
    </Context.Provider>
  );
}

export function FieldError({ id, children }: { id?: string; children: ReactNode }) {
  return (
    <p id={id} className="flex items-start gap-1.5 text-xs leading-5 text-red-600 dark:text-red-400" role="alert">
      <CircleAlert size={14} className="mt-0.5 shrink-0" aria-hidden />
      <span>{children}</span>
    </p>
  );
}

/** A group of related fields with a heading. */
export function Fieldset({ legend, description, children, className }: { legend: ReactNode; description?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <fieldset className={cx('min-w-0 space-y-4', className)}>
      <legend className="text-sm font-semibold text-neutral-900 dark:text-neutral-50">{legend}</legend>
      {description && <p className="-mt-2 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{description}</p>}
      {children}
    </fieldset>
  );
}
