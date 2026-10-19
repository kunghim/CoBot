// CreateTeamDialog collects a working directory, a name, a scenario and the
// members, then calls POST /teams. "Choose folder" opens the operating
// system's folder chooser (through the desktop bridge, or the local core in
// the browser); a path can still be typed or pasted. The chosen directory is
// checked at once with the same rules creation uses; a folder another team
// claims cannot be used. Backend validation errors (duplicate/nested
// directory, member limit, unknown role...) surface through their messageKey
// with params.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useCreateTeam, useRoles } from '../../api/queries';
import { api } from '../../api/client';
import type { DirectoryCheckResult } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, Button, CheckboxCards, cx, Dialog, Field, Fieldset, Icons, SegmentedControl, Skeleton, Spinner, TextInput } from '../../components/ui';
import { pickDirectory } from '../../platform/pickDirectory';
import Avatar from '../avatars/Avatar';

/** folderName is the last path segment, used as the suggested team name. */
function folderName(path: string): string {
  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/);
  return parts[parts.length - 1] ?? '';
}

export default function CreateTeamDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (leaderBotId: string, teamId: string) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const roles = useRoles(open);
  const create = useCreateTeam();

  const [name, setName] = useState('');
  const [nameEdited, setNameEdited] = useState(false);
  const [directory, setDirectory] = useState('');
  const [scenarioId, setScenarioId] = useState('');
  const [selectedRoles, setSelectedRoles] = useState<ReadonlySet<string>>(() => new Set());
  const [errorText, setErrorText] = useState('');
  const [verdict, setVerdict] = useState<DirectoryCheckResult | null>(null);
  const [checking, setChecking] = useState(false);
  const [picking, setPicking] = useState(false);
  const checkedPath = useRef('');

  const scenarios = roles.data?.scenarios ?? [];
  const scenario = scenarios.find((entry) => entry.id === scenarioId) ?? scenarios[0];

  // Fresh form each time the dialog opens.
  useEffect(() => {
    if (!open) return;
    setName('');
    setNameEdited(false);
    setDirectory('');
    setScenarioId('');
    setSelectedRoles(new Set());
    setErrorText('');
    setVerdict(null);
    checkedPath.current = '';
  }, [open]);

  // A scenario preselects its usual members; switching scenarios resets them.
  useEffect(() => {
    if (open && scenario) setSelectedRoles(new Set(scenario.defaultRoleIds));
  }, [open, scenario]);

  if (!open) return null;

  const library = roles.data?.items ?? [];
  const leader = library.find((role) => role.id === 'leader');
  const offered = scenario
    ? scenario.roleIds.map((id) => library.find((role) => role.id === id)).filter((role) => role !== undefined)
    : library.filter((role) => role.id !== 'leader');
  const canSubmit = name.trim() !== '' && directory.trim() !== '' && verdict?.ok !== false && !create.isPending;

  // The check uses the same rules creation uses, so a duplicate, nested or
  // foreign-marked directory shows before submit, not only as a late error.
  const checkDirectory = (path: string) => {
    const trimmed = path.trim();
    if (!trimmed || trimmed === checkedPath.current) return;
    checkedPath.current = trimmed;
    setChecking(true);
    setVerdict(null);
    setErrorText('');
    api
      .checkDirectory(trimmed)
      .then((result) => setVerdict(result))
      .catch((error) => setErrorText(apiErrorMessage(t, error)))
      .finally(() => setChecking(false));
  };

  const applyDirectory = (path: string) => {
    setDirectory(path);
    if (!nameEdited) setName(folderName(path));
    checkDirectory(path);
  };

  const choose = () => {
    setErrorText('');
    setPicking(true);
    pickDirectory()
      .then((path) => {
        if (path) applyDirectory(path);
      })
      .catch((error) => setErrorText(apiErrorMessage(t, error)))
      .finally(() => setPicking(false));
  };

  const verdictText = (result: DirectoryCheckResult): string => {
    const teamName = result.teamName ?? '';
    if (result.ok) return t('team.directoryOk', { path: result.canonical });
    if (result.duplicate && result.archived) return t('team.directoryArchived', { teamName });
    if (result.duplicate) return t('team.directoryDuplicate', { teamName });
    if (result.nested) return t('team.directoryNested', { teamName });
    if (result.claimed) return teamName ? t('team.directoryClaimed', { teamName }) : t('team.directoryClaimedUnknown');
    return t('status.error');
  };

  const toggleRole = (roleId: string) => {
    setSelectedRoles((prev) => {
      const next = new Set(prev);
      if (next.has(roleId)) next.delete(roleId);
      else next.add(roleId);
      return next;
    });
  };

  const submit = () => {
    if (!canSubmit) return;
    setErrorText('');
    create.mutate(
      { name: name.trim(), directory: directory.trim(), initialRoleIds: [...selectedRoles] },
      {
        onSuccess: (result) => onCreated(result.team.leaderId, result.team.id),
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  const roleOption = (id: string, label: string, summary: string, isLeader: boolean) => ({
    value: id,
    label,
    description: summary || undefined,
    media: <Avatar bot={{ roleId: id, isLeader }} />,
  });

  return (
    <Dialog
      open
      size="lg"
      onClose={onClose}
      busy={create.isPending}
      closeLabel={t('common.close')}
      title={t('team.createTitle')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={create.isPending}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" onClick={submit} loading={create.isPending} disabled={!canSubmit}>
            {create.isPending ? t('team.creating') : t('team.submit')}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <Field label={t('team.directory')}>
          <div className="flex gap-2">
            <Button variant="secondary" icon={Icons.openFolder} onClick={choose} loading={picking} className="shrink-0">
              {picking ? t('team.choosing') : t('team.chooseDirectory')}
            </Button>
            <TextInput
              mono
              className="min-w-0 flex-1"
              value={directory}
              onChange={(event) => {
                setDirectory(event.target.value);
                setVerdict(null);
              }}
              onBlur={() => {
                if (directory.trim()) applyDirectory(directory.trim());
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && directory.trim()) applyDirectory(directory.trim());
              }}
              placeholder={t('team.directoryPlaceholder')}
            />
          </div>
          {/* One line is kept for the check result, so the dialog does not
              shift under the pointer when leaving the field starts a check. */}
          <div className="min-h-[1.25rem]">
            {checking && (
              <p role="status" className="flex items-center gap-1.5 text-xs text-neutral-500 dark:text-neutral-400">
                <Spinner size={12} />
                {t('status.loading')}
              </p>
            )}
            {verdict && (
              <p role="status" className={cx('flex items-start gap-1.5 break-all text-xs leading-5', verdict.ok ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400')}>
                {verdict.ok ? <Icons.success size={14} className="mt-0.5 shrink-0" aria-hidden /> : <Icons.warning size={14} className="mt-0.5 shrink-0" aria-hidden />}
                <span>
                  {verdictText(verdict)}
                  {verdict.duplicate && verdict.archived && verdict.teamId && (
                    <button
                      type="button"
                      onClick={() => {
                        onClose();
                        navigate(`/settings/archived?team=${encodeURIComponent(verdict.teamId ?? '')}`);
                      }}
                      className="ml-2 font-medium underline underline-offset-2"
                    >
                      {t('team.goRestore')}
                    </button>
                  )}
                </span>
              </p>
            )}
          </div>
        </Field>

        <Field label={t('team.name')}>
          <TextInput
            value={name}
            onChange={(event) => {
              setName(event.target.value);
              setNameEdited(true);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') submit();
            }}
            placeholder={t('team.namePlaceholder')}
          />
        </Field>

        {scenarios.length > 1 && (
          <Field label={t('team.scenario')}>
            <div>
              <SegmentedControl
                label={t('team.scenario')}
                value={scenario?.id ?? ''}
                onChange={setScenarioId}
                options={scenarios.map((entry) => ({ value: entry.id, label: t(entry.nameKey, { defaultValue: entry.name }) }))}
              />
            </div>
          </Field>
        )}

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

        {errorText && <Alert tone="danger">{errorText}</Alert>}
      </div>
    </Dialog>
  );
}
