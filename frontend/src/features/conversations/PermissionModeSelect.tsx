// PermissionModeSelect is the composer's permission switch, like Codex's:
// read-only, default (free inside the team folder, asks beyond it) or full
// access. It belongs to the team and applies from each member's next turn;
// full access asks for confirmation first.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ShieldCheck } from 'lucide-react';
import { useSetTeamPermissionMode } from '../../api/queries';
import type { PermissionMode, Team } from '../../api/types';
import ConfirmDialog from '../../components/ConfirmDialog';
import { apiErrorMessage } from '../../i18n/errors';

const modes: PermissionMode[] = ['read_only', 'default', 'full'];

export default function PermissionModeSelect({ team, disabled }: { team: Team; disabled?: boolean }) {
  const { t } = useTranslation();
  const setMode = useSetTeamPermissionMode();
  const [confirmFull, setConfirmFull] = useState(false);
  const [error, setError] = useState('');
  const current: PermissionMode = modes.includes(team.permissionMode as PermissionMode) ? (team.permissionMode as PermissionMode) : 'default';

  const apply = (mode: PermissionMode) => {
    setError('');
    setMode.mutate(
      { teamId: team.id, mode, expectedRevision: team.revision },
      {
        onSuccess: () => setConfirmFull(false),
        onError: (err) => setError(apiErrorMessage(t, err)),
      },
    );
  };

  return (
    <div className="flex min-w-0 items-center gap-1.5 text-xs text-neutral-500 dark:text-neutral-400">
      <ShieldCheck size={13} aria-hidden="true" className={current === 'full' ? 'text-red-500' : undefined} />
      <label className="sr-only" htmlFor={`permission-${team.id}`}>{t('permission.label')}</label>
      <select
        id={`permission-${team.id}`}
        value={current}
        disabled={disabled || setMode.isPending}
        title={t(`permission.hint.${current}`)}
        onChange={(event) => {
          const mode = event.target.value as PermissionMode;
          if (mode === 'full') setConfirmFull(true);
          else apply(mode);
        }}
        className={`rounded border-0 bg-transparent py-0.5 pr-6 text-xs hover:text-neutral-900 focus:ring-1 focus:ring-neutral-400 disabled:opacity-60 dark:hover:text-neutral-100 ${current === 'full' ? 'text-red-600 dark:text-red-400' : ''}`}
      >
        {modes.map((mode) => (
          <option key={mode} value={mode}>
            {t(`permission.mode.${mode}`)}
          </option>
        ))}
      </select>
      <span className="hidden truncate sm:inline">{t(`permission.hint.${current}`)}</span>
      {error && !confirmFull && <span role="alert" className="text-red-600">{error}</span>}
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
