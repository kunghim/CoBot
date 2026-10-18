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
import { Check, FolderOpen, Lock } from 'lucide-react';
import { useCreateTeam, useRoles } from '../../api/queries';
import { api } from '../../api/client';
import type { DirectoryCheckResult } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { useDialogFocus } from '../../components/useDialogFocus';
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

  const dialogRef = useDialogFocus(open, onClose);

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

  const roleCard = (id: string, label: string, summary: string, selected: boolean, locked: boolean) => (
    <button
      key={id}
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-disabled={locked}
      onClick={() => {
        if (!locked) toggleRole(id);
      }}
      className={`flex items-start gap-2.5 rounded-xl border px-3 py-2.5 text-left transition-colors ${
        selected
          ? 'border-neutral-900 bg-neutral-50 dark:border-neutral-200 dark:bg-neutral-800'
          : 'border-neutral-200 hover:border-neutral-400 dark:border-neutral-700 dark:hover:border-neutral-500'
      } ${locked ? 'cursor-default' : ''}`}
    >
      <span
        aria-hidden="true"
        className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
          selected
            ? 'border-neutral-900 bg-neutral-900 text-white dark:border-neutral-100 dark:bg-neutral-100 dark:text-neutral-900'
            : 'border-neutral-300 dark:border-neutral-600'
        }`}
      >
        {locked ? <Lock size={10} /> : selected ? <Check size={12} /> : null}
      </span>
      <Avatar bot={{ roleId: id, isLeader: locked }} />
      <span className="min-w-0">
        <span className="flex items-center gap-1.5 text-sm font-medium">
          {label}
          {locked && (
            <span className="rounded bg-neutral-200 px-1 text-[10px] font-normal text-neutral-600 dark:bg-neutral-700 dark:text-neutral-300">
              {t('team.required')}
            </span>
          )}
        </span>
        {summary && <span className="mt-0.5 block text-xs text-neutral-500 dark:text-neutral-400">{summary}</span>}
      </span>
    </button>
  );

  return (
    <div className="fixed inset-0 z-30 flex items-start justify-center overflow-y-auto bg-neutral-950/45 p-4 pt-[10vh] backdrop-blur-[2px]" onMouseDown={onClose}>
      <div
        ref={dialogRef as React.RefObject<HTMLDivElement>}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-team-title"
        onMouseDown={(event) => event.stopPropagation()}
        className="w-full max-w-xl rounded-2xl border border-neutral-200 bg-white p-6 shadow-2xl dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id="create-team-title" className="text-lg font-semibold tracking-tight">
          {t('team.createTitle')}
        </h2>

        <div className="mt-4 text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.directory')}</span>
          <div className="mt-1 flex gap-2">
            <button
              type="button"
              onClick={choose}
              disabled={picking}
              className="flex shrink-0 items-center gap-1.5 rounded-md bg-neutral-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
            >
              <FolderOpen size={15} aria-hidden="true" />
              {picking ? t('team.choosing') : t('team.chooseDirectory')}
            </button>
            <input
              aria-label={t('team.directory')}
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
              className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 font-mono text-xs focus:border-signal-500 focus:outline-none dark:border-neutral-700"
            />
          </div>
          {/* One line is kept for the check result, so the dialog does not
              shift under the pointer when leaving the field starts a check. */}
          <div className="mt-1.5 min-h-[1.125rem]">
          {checking && (
            <p role="status" className="text-xs text-neutral-500">
              {t('status.loading')}
            </p>
          )}
          {verdict && (
            <p
              role="status"
              className={`break-all text-xs ${verdict.ok ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'}`}
            >
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
            </p>
          )}
          </div>
        </div>

        <label className="mt-3 block text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.name')}</span>
          <input
            value={name}
            onChange={(event) => {
              setName(event.target.value);
              setNameEdited(true);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') submit();
            }}
            placeholder={t('team.namePlaceholder')}
            className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm focus:border-signal-500 focus:outline-none dark:border-neutral-700"
          />
        </label>

        {scenarios.length > 0 && (
          <div className="mt-4 text-sm">
            <span className="text-neutral-500 dark:text-neutral-400">{t('team.scenario')}</span>
            <div role="radiogroup" aria-label={t('team.scenario')} className="mt-1 flex flex-wrap gap-2">
              {scenarios.map((entry) => {
                const active = entry.id === scenario?.id;
                return (
                  <button
                    key={entry.id}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setScenarioId(entry.id)}
                    className={`rounded-full border px-3 py-1 text-sm ${
                      active
                        ? 'border-neutral-900 bg-neutral-900 text-white dark:border-neutral-100 dark:bg-neutral-100 dark:text-neutral-900'
                        : 'border-neutral-300 text-neutral-600 hover:border-neutral-500 dark:border-neutral-700 dark:text-neutral-300'
                    }`}
                  >
                    {t(entry.nameKey, { defaultValue: entry.name })}
                  </button>
                );
              })}
            </div>
          </div>
        )}

        <fieldset className="mt-4">
          <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('team.members')}</legend>
          {roles.isPending && (
            <p className="mt-1 text-xs text-neutral-400" role="status">
              {t('status.loading')}
            </p>
          )}
          {roles.isError && (
            <p className="mt-1 text-xs text-red-600" role="alert">
              {t('status.error')}
            </p>
          )}
          {roles.isSuccess && (
            <div className="mt-1 grid gap-2 sm:grid-cols-2">
              {leader &&
                roleCard(
                  leader.id,
                  t(leader.nameKey, { defaultValue: leader.name }),
                  leader.summaryKey ? t(leader.summaryKey, { defaultValue: '' }) : '',
                  true,
                  true,
                )}
              {offered.map((role) =>
                roleCard(
                  role.id,
                  t(role.nameKey, { defaultValue: role.name }),
                  role.summaryKey ? t(role.summaryKey, { defaultValue: '' }) : '',
                  selectedRoles.has(role.id),
                  false,
                ),
              )}
            </div>
          )}
        </fieldset>

        {errorText && (
          <p className="mt-3 text-sm text-red-600" role="alert">
            {errorText}
          </p>
        )}

        <div className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100"
          >
            {t('team.cancel')}
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={!canSubmit}
            className="rounded-md bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {create.isPending ? t('team.creating') : t('team.submit')}
          </button>
        </div>
      </div>
    </div>
  );
}
