// OpenTeamDialog picks a project folder and resolves what the core can do
// with it: jump to a live team, restore an archived one, adopt a folder
// another CoBot environment marked (with the user's explicit confirmation,
// since adoption rewrites that marker), or create a team in an unmarked
// folder with the directory and name prefilled.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { api } from '../../api/client';
import type { OpenTeamResult } from '../../api/types';
import { useAdoptTeam, useRestoreTeam, useRoles } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, Button, CheckboxCards, cx, Dialog, Field, Fieldset, Icons, Skeleton, Spinner, TextInput } from '../../components/ui';
import { pickDirectory } from '../../platform/pickDirectory';
import Avatar from '../avatars/Avatar';

export default function OpenTeamDialog({
  open,
  onClose,
  onOpened,
}: {
  open: boolean;
  onClose: () => void;
  onOpened: (leaderBotId: string, teamId: string) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const roles = useRoles(open);
  const restore = useRestoreTeam();
  const adopt = useAdoptTeam();

  const [path, setPath] = useState('');
  const [resolved, setResolved] = useState<OpenTeamResult | null>(null);
  const [resolving, setResolving] = useState(false);
  const [picking, setPicking] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [selectedRoles, setSelectedRoles] = useState<ReadonlySet<string>>(() => new Set());
  const checkedPath = useRef('');

  const library = roles.data?.items ?? [];
  const leader = library.find((role) => role.id === 'leader');
  const offered = library.filter((role) => role.id !== 'leader');

  useEffect(() => {
    if (!open) return;
    setPath('');
    setResolved(null);
    setErrorText('');
    setConfirming(false);
    setSelectedRoles(new Set());
    checkedPath.current = '';
  }, [open]);

  if (!open) return null;

  const resolve = (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed || trimmed === checkedPath.current) return;
    checkedPath.current = trimmed;
    setResolving(true);
    setResolved(null);
    setConfirming(false);
    setErrorText('');
    api
      .openTeam(trimmed)
      .then((result) => setResolved(result))
      .catch((error) => setErrorText(apiErrorMessage(t, error)))
      .finally(() => setResolving(false));
  };

  const choose = () => {
    setErrorText('');
    setPicking(true);
    pickDirectory()
      .then((picked) => {
        if (picked) {
          setPath(picked);
          resolve(picked);
        }
      })
      .catch((error) => setErrorText(apiErrorMessage(t, error)))
      .finally(() => setPicking(false));
  };

  // Opening or restoring lands on the team: the list resolves its leader
  // from the snapshot it already holds, so this dialog only reports the id.
  const landOn = (teamId: string) => {
    onClose();
    onOpened('', teamId);
    navigate('/');
  };

  const doRestore = () => {
    if (!resolved?.teamId) return;
    setErrorText('');
    restore.mutate(
      { teamId: resolved.teamId },
      {
        onSuccess: () => landOn(resolved.teamId!),
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  const doAdopt = () => {
    if (!resolved) return;
    setErrorText('');
    adopt.mutate(
      { path: resolved.directory, name: resolved.suggestedName ?? '', initialRoleIds: [...selectedRoles] },
      {
        onSuccess: (created) => {
          onClose();
          onOpened(created.team.leaderId, created.team.id);
          navigate('/');
        },
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  const doCreate = () => {
    if (!resolved) return;
    onClose();
    navigate(`/team/new?directory=${encodeURIComponent(resolved.directory)}&name=${encodeURIComponent(resolved.suggestedName ?? '')}`);
  };

  const actionText = (result: OpenTeamResult): string => {
    const name = result.teamName ?? result.suggestedName ?? '';
    switch (result.action) {
      case 'open':
        return t('team.openAction.open', { name: result.teamName ?? '' });
      case 'restore':
        return t('team.openAction.restore', { name: result.teamName ?? '' });
      case 'adopt':
        return t('team.openAction.adopt', { name });
      default:
        return t('team.openAction.create', { name: result.suggestedName ?? '' });
    }
  };

  const toggleRole = (roleId: string) => {
    setSelectedRoles((prev) => {
      const next = new Set(prev);
      if (next.has(roleId)) next.delete(roleId);
      else next.add(roleId);
      return next;
    });
  };

  const roleOption = (id: string, label: string, summary: string, isLeader: boolean) => ({
    value: id,
    label,
    description: summary || undefined,
    media: <Avatar bot={{ roleId: id, isLeader }} />,
  });

  const needRolePick = resolved?.action === 'adopt';

  return (
    <Dialog
      open
      size="md"
      onClose={onClose}
      busy={restore.isPending || adopt.isPending}
      closeLabel={t('common.close')}
      title={t('team.openTitle')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          {resolved?.action === 'restore' && (
            <Button variant="primary" loading={restore.isPending} onClick={doRestore}>
              {t('team.openRestoreButton')}
            </Button>
          )}
          {resolved?.action === 'adopt' && (
            <Button
              variant="primary"
              loading={adopt.isPending}
              disabled={!confirming || (needRolePick && selectedRoles.size === 0)}
              onClick={doAdopt}
            >
              {t('team.openAdoptButton')}
            </Button>
          )}
          {resolved?.action === 'create' && (
            <Button variant="primary" onClick={doCreate}>
              {t('team.openCreateButton')}
            </Button>
          )}
        </>
      }
    >
      <div className="space-y-5">
        <Field label={t('team.directory')}>
          <div className="flex gap-2">
            <TextInput
              mono
              className="min-w-0 flex-1"
              value={path}
              onChange={(event) => {
                setPath(event.target.value);
                setResolved(null);
                setConfirming(false);
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && path.trim()) resolve(path.trim());
              }}
              placeholder={t('team.directoryPlaceholder')}
            />
            <Button variant="secondary" icon={Icons.openFolder} onClick={choose} loading={picking} className="shrink-0">
              {picking ? t('team.choosing') : t('team.chooseDirectory')}
            </Button>
          </div>
        </Field>

        {resolving && (
          <p role="status" className="flex items-center gap-1.5 text-xs text-neutral-500 dark:text-neutral-400">
            <Spinner size={12} />
            {t('status.loading')}
          </p>
        )}

        {resolved && (
          <div className="space-y-3">
            <p className={cx('flex items-start gap-1.5 break-all text-sm leading-5', resolved.action === 'open' ? 'text-emerald-700 dark:text-emerald-400' : 'text-neutral-800 dark:text-neutral-100')}>
              {resolved.action === 'open' ? <Icons.success size={15} className="mt-0.5 shrink-0" aria-hidden /> : <Icons.team size={15} className="mt-0.5 shrink-0" aria-hidden />}
              {actionText(resolved)}
            </p>
            {resolved.action === 'adopt' && (
              <>
                <Alert tone="warning">{t('team.adoptWarning')}</Alert>
                <label className="flex items-start gap-2 text-sm text-neutral-800 dark:text-neutral-100">
                  <input type="checkbox" checked={confirming} onChange={(event) => setConfirming(event.target.checked)} className="mt-0.5" />
                  {t('team.adoptConfirm')}
                </label>
                <Fieldset legend={t('team.members')}>
                  {roles.isPending && <Skeleton className="h-24 w-full" />}
                  {roles.isError && <Alert tone="danger">{t('status.error')}</Alert>}
                  {roles.isSuccess && (
                    <CheckboxCards
                      label={t('team.members')}
                      values={selectedRoles}
                      onToggle={toggleRole}
                      locked={leader ? [leader.id] : []}
                      lockedLabel={t('team.required')}
                      options={[
                        ...(leader ? [roleOption(leader.id, t(leader.nameKey, { defaultValue: leader.name }), leader.summaryKey ? t(leader.summaryKey, { defaultValue: '' }) : '', true)] : []),
                        ...offered.map((role) => roleOption(role.id, t(role.nameKey, { defaultValue: role.name }), role.summaryKey ? t(role.summaryKey, { defaultValue: '' }) : '', false)),
                      ]}
                    />
                  )}
                </Fieldset>
              </>
            )}
          </div>
        )}

        {errorText && <Alert tone="danger">{errorText}</Alert>}
      </div>
    </Dialog>
  );
}
