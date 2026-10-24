// PermissionModeSelect is the composer's permission switch, like Codex's: a
// small pill that opens a menu of read only, default and full access. The
// mode belongs to the team and applies from each member's next turn; full
// access asks for confirmation first. A chat uses the same menu for its own
// mode (PermissionModeMenu), which applies from the next message.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSetTeamPermissionMode } from '../../api/queries';
import type { PermissionMode, Team } from '../../api/types';
import { AlertDialog, ComposerOption, Icons, Menu } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';

const modes: PermissionMode[] = ['read_only', 'default', 'full'];
const icons = { read_only: Icons.readOnly, default: Icons.permission, full: Icons.fullAccess } as const;

/** The mode as the menu shows it: anything unknown reads as the default. */
export function permissionModeOf(value: string | undefined): PermissionMode {
  return modes.includes(value as PermissionMode) ? (value as PermissionMode) : 'default';
}

/**
 * The menu itself (`kind` says whose words: a team's or a chat's). `onChange` takes the mode chosen (after the confirmation
 * full access asks for) and may be asynchronous: the menu shows its failure.
 */
export function PermissionModeMenu({ value, onChange, disabled, kind = 'team' }: { value: PermissionMode; onChange: (mode: PermissionMode) => void | Promise<unknown>; disabled?: boolean; kind?: 'team' | 'chat' }) {
  const { t } = useTranslation();
  const [confirmFull, setConfirmFull] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // A chat has no team folder and no members: the words that name them are the team's.
  const words = kind === 'chat' ? 'permission.chat' : 'permission';

  const apply = async (mode: PermissionMode) => {
    setError('');
    setBusy(true);
    try {
      await onChange(mode);
      setConfirmFull(false);
    } catch (err) {
      setError(apiErrorMessage(t, err));
    } finally {
      setBusy(false);
    }
  };

  const choose = (mode: PermissionMode) => {
    if (mode === value) return;
    if (mode === 'full') setConfirmFull(true);
    else void apply(mode);
  };

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Menu
        label={t('permission.label')}
        placement="top-start"
        items={modes.map((mode) => ({
          id: mode,
          label: t(`permission.mode.${mode}`),
          description: t(`${words}.hint.${mode}`),
          icon: icons[mode],
          checked: mode === value,
          onSelect: () => choose(mode),
        }))}
        trigger={
          <ComposerOption
            icon={icons[value]}
            tone={value === 'full' ? 'danger' : 'neutral'}
            disabled={disabled || busy}
            aria-label={`${t('permission.label')}：${t(`permission.mode.${value}`)}`}
          >
            {t(`permission.mode.${value}`)}
          </ComposerOption>
        }
      />
      {error && !confirmFull && <span role="alert" className="truncate text-xs text-red-600 dark:text-red-400">{error}</span>}
      <AlertDialog
        open={confirmFull}
        title={t('permission.confirmFull.title')}
        description={t(`${words}.confirmFull.body`)}
        confirmLabel={t('permission.confirmFull.confirm')}
        cancelLabel={t('common.cancel')}
        busy={busy}
        error={confirmFull && error ? error : undefined}
        onConfirm={() => void apply('full')}
        onCancel={() => {
          setConfirmFull(false);
          setError('');
        }}
      />
    </div>
  );
}

export default function PermissionModeSelect({ team, disabled }: { team: Team; disabled?: boolean }) {
  const setMode = useSetTeamPermissionMode();
  return (
    <PermissionModeMenu
      value={permissionModeOf(team.permissionMode)}
      disabled={disabled}
      onChange={(mode) => setMode.mutateAsync({ teamId: team.id, mode, expectedRevision: team.revision })}
    />
  );
}
