// PermissionModeSelect is the composer's permission switch, like Codex's: a
// small pill that opens a menu of read only, default and full access. The
// mode belongs to the team and applies from each member's next turn; full
// access asks for confirmation first.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSetTeamPermissionMode } from '../../api/queries';
import type { PermissionMode, Team } from '../../api/types';
import { AlertDialog, ComposerOption, Icons, Menu } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';

const modes: PermissionMode[] = ['read_only', 'default', 'full'];
const icons = { read_only: Icons.readOnly, default: Icons.permission, full: Icons.fullAccess } as const;

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
      { onSuccess: () => setConfirmFull(false), onError: (err) => setError(apiErrorMessage(t, err)) },
    );
  };

  const choose = (mode: PermissionMode) => {
    if (mode === current) return;
    if (mode === 'full') setConfirmFull(true);
    else apply(mode);
  };

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Menu
        label={t('permission.label')}
        placement="top-start"
        items={modes.map((mode) => ({
          id: mode,
          label: t(`permission.mode.${mode}`),
          description: t(`permission.hint.${mode}`),
          icon: icons[mode],
          checked: mode === current,
          onSelect: () => choose(mode),
        }))}
        trigger={
          <ComposerOption
            icon={icons[current]}
            tone={current === 'full' ? 'danger' : 'neutral'}
            disabled={disabled || setMode.isPending}
            aria-label={`${t('permission.label')}：${t(`permission.mode.${current}`)}`}
          >
            {t(`permission.mode.${current}`)}
          </ComposerOption>
        }
      />
      {error && !confirmFull && <span role="alert" className="truncate text-xs text-red-600 dark:text-red-400">{error}</span>}
      <AlertDialog
        open={confirmFull}
        title={t('permission.confirmFull.title')}
        description={t('permission.confirmFull.body')}
        confirmLabel={t('permission.confirmFull.confirm')}
        cancelLabel={t('common.cancel')}
        busy={setMode.isPending}
        error={confirmFull && error ? error : undefined}
        onConfirm={() => apply('full')}
        onCancel={() => {
          setConfirmFull(false);
          setError('');
        }}
      />
    </div>
  );
}
