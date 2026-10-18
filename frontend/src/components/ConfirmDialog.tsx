import { useTranslation } from 'react-i18next';
import { useDialogFocus } from './useDialogFocus';

/**
 * ConfirmDialog asks before an action with consequences the user should see
 * first. The confirm button carries the action's own name, so the choice
 * reads plainly; it is red only for actions that cannot be undone. Escape or
 * the backdrop cancels, and both stay disabled while the action runs.
 */
export default function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  pending = false,
  errorText = '',
  danger = true,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  pending?: boolean;
  errorText?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const dialogRef = useDialogFocus(open, onCancel, pending);
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onMouseDown={() => {
        if (!pending) onCancel();
      }}
    >
      <div
        ref={dialogRef as React.RefObject<HTMLDivElement>}
        tabIndex={-1}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        aria-describedby="confirm-dialog-body"
        onMouseDown={(event) => event.stopPropagation()}
        className="w-full max-w-sm rounded-xl border border-neutral-200 bg-white p-5 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id="confirm-dialog-title" className="text-sm font-semibold">
          {title}
        </h2>
        <p id="confirm-dialog-body" className="mt-2 whitespace-pre-line text-sm text-neutral-600 dark:text-neutral-300">
          {body}
        </p>
        {errorText && (
          <p className="mt-3 text-sm text-red-600" role="alert">
            {errorText}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={pending}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 disabled:opacity-50 dark:hover:text-neutral-100"
          >
            {t('team.cancel')}
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={pending}
            className={`rounded-md px-4 py-1.5 text-sm font-medium disabled:opacity-50 ${
              danger
                ? 'bg-red-600 text-white hover:bg-red-700'
                : 'bg-neutral-900 text-white hover:bg-neutral-700 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300'
            }`}
          >
            {pending ? t('status.loading') : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
