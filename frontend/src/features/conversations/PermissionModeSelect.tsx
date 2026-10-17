// PermissionModeSelect is the composer's permission switch, like Codex's: a
// small pill that opens a menu of read only, default and full access. The
// mode belongs to the team and applies from each member's next turn; full
// access asks for confirmation first.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, ChevronDown, ShieldAlert, ShieldCheck, ShieldEllipsis } from 'lucide-react';
import { useSetTeamPermissionMode } from '../../api/queries';
import type { PermissionMode, Team } from '../../api/types';
import ConfirmDialog from '../../components/ConfirmDialog';
import { apiErrorMessage } from '../../i18n/errors';

const modes: PermissionMode[] = ['read_only', 'default', 'full'];
const icons = { read_only: ShieldEllipsis, default: ShieldCheck, full: ShieldAlert } as const;

export default function PermissionModeSelect({ team, disabled }: { team: Team; disabled?: boolean }) {
  const { t } = useTranslation();
  const setMode = useSetTeamPermissionMode();
  const [open, setOpen] = useState(false);
  const [confirmFull, setConfirmFull] = useState(false);
  const [error, setError] = useState('');
  const rootRef = useRef<HTMLDivElement>(null);
  const current: PermissionMode = modes.includes(team.permissionMode as PermissionMode) ? (team.permissionMode as PermissionMode) : 'default';
  const Icon = icons[current];

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent | KeyboardEvent) => {
      if (event instanceof KeyboardEvent ? event.key === 'Escape' : !rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', close);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', close);
    };
  }, [open]);

  const apply = (mode: PermissionMode) => {
    setError('');
    setMode.mutate(
      { teamId: team.id, mode, expectedRevision: team.revision },
      { onSuccess: () => setConfirmFull(false), onError: (err) => setError(apiErrorMessage(t, err)) },
    );
  };

  const choose = (mode: PermissionMode) => {
    setOpen(false);
    if (mode === current) return;
    if (mode === 'full') setConfirmFull(true);
    else apply(mode);
  };

  return (
    <div ref={rootRef} className="relative flex min-w-0 items-center gap-2">
      <button
        type="button"
        disabled={disabled || setMode.isPending}
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${t('permission.label')}：${t(`permission.mode.${current}`)}`}
        className={`inline-flex h-7 items-center gap-1 rounded-full px-2.5 text-xs font-medium transition-colors disabled:opacity-50 ${
          current === 'full'
            ? 'text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/50'
            : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800 dark:hover:text-neutral-100'
        }`}
      >
        <Icon size={14} aria-hidden="true" />
        {t(`permission.mode.${current}`)}
        <ChevronDown size={12} aria-hidden="true" className="opacity-60" />
      </button>
      {open && (
        <div role="menu" aria-label={t('permission.label')} className="absolute bottom-full left-0 z-20 mb-2 w-72 rounded-xl border border-neutral-200 bg-white p-1 shadow-lg dark:border-neutral-700 dark:bg-neutral-900">
          {modes.map((mode) => {
            const ItemIcon = icons[mode];
            return (
              <button
                key={mode}
                type="button"
                role="menuitemradio"
                aria-checked={mode === current}
                onClick={() => choose(mode)}
                className="flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left hover:bg-neutral-100 dark:hover:bg-neutral-800"
              >
                <ItemIcon size={16} aria-hidden="true" className={`mt-0.5 shrink-0 ${mode === 'full' ? 'text-red-500' : 'text-neutral-500'}`} />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-neutral-900 dark:text-neutral-100">{t(`permission.mode.${mode}`)}</span>
                  <span className="block text-xs text-neutral-500 dark:text-neutral-400">{t(`permission.hint.${mode}`)}</span>
                </span>
                {mode === current && <Check size={14} aria-hidden="true" className="mt-1 shrink-0 text-neutral-700 dark:text-neutral-200" />}
              </button>
            );
          })}
        </div>
      )}
      {error && !confirmFull && <span role="alert" className="truncate text-xs text-red-600">{error}</span>}
      <ConfirmDialog
        open={confirmFull}
        title={t('permission.confirmFull.title')}
        body={t('permission.confirmFull.body')}
        confirmLabel={t('permission.confirmFull.confirm')}
        pending={setMode.isPending}
        errorText={confirmFull ? error : ''}
        onConfirm={() => apply('full')}
        onCancel={() => {
          setConfirmFull(false);
          setError('');
        }}
      />
    </div>
  );
}
