// CreateTeamDialog collects name, working directory and optional initial
// members, then calls POST /teams. The desktop shell offers the native
// directory picker through the bridge; in the browser the path is a plain
// text input. Backend validation errors (duplicate/nested directory, member
// limit, unknown role...) surface through their messageKey with params.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { FolderOpen } from 'lucide-react';
import { useCreateTeam, useRoles } from '../../api/queries';
import { api } from '../../api/client';
import type { DirectoryCheckResult } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { desktopBridge } from '../../platform/native-bridge';

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
  const roles = useRoles(open);
  const create = useCreateTeam();

  const [name, setName] = useState('');
  const [directory, setDirectory] = useState('');
  const [selectedRoles, setSelectedRoles] = useState<ReadonlySet<string>>(() => new Set());
  const [errorText, setErrorText] = useState('');
  const [verdict, setVerdict] = useState<DirectoryCheckResult | null>(null);
  const [children, setChildren] = useState<string[]>([]);
  const [adoptMarker, setAdoptMarker] = useState(false);
  const [checking, setChecking] = useState(false);

  // Fresh form each time the dialog opens.
  useEffect(() => {
    if (!open) return;
    setName('');
    setDirectory('');
    setSelectedRoles(new Set());
    setErrorText('');
    setVerdict(null);
    setChildren([]);
    setAdoptMarker(false);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  const bridge = desktopBridge();
  const selectableRoles = (roles.data?.items ?? []).filter((role) => role.id !== 'leader');
  const canSubmit = name.trim() !== '' && directory.trim() !== '' && !create.isPending;

  const browse = () => {
    if (!bridge) return;
    void bridge
      .OpenDirectory()
      .then((picked) => {
        if (picked) setDirectory(picked);
      })
      .catch(() => {
        /* picker dismissed or unavailable: keep the text input */
      });
  };

  // The browser picker validates through the same rules creation uses, so a
  // duplicate, nested, or foreign-marked directory is visible before submit
  // instead of only as a late error.
  const checkDirectory = () => {
    if (!directory.trim()) return;
    setChecking(true);
    setVerdict(null);
    api
      .checkDirectory(directory.trim())
      .then((result) => {
        setVerdict(result);
        setAdoptMarker(false);
      })
      .catch((error) => setErrorText(apiErrorMessage(t, error)))
      .finally(() => setChecking(false));
  };

  const listChildren = () => {
    api
      .listDirectories(directory.trim() || undefined)
      .then((result) => {
        setChildren(result.children);
        if (result.path && !directory.trim()) setDirectory(result.path);
      })
      .catch((error) => setErrorText(apiErrorMessage(t, error)));
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
      { name: name.trim(), directory: directory.trim(), initialRoleIds: [...selectedRoles], adoptMarker },
      {
        onSuccess: (result) => onCreated(result.team.leaderId, result.team.id),
        onError: (error) => setErrorText(apiErrorMessage(t, error)),
      },
    );
  };

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-black/40 p-4" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-team-title"
        onMouseDown={(event) => event.stopPropagation()}
        className="w-full max-w-md rounded-xl border border-neutral-200 bg-white p-5 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id="create-team-title" className="text-sm font-semibold">
          {t('team.createTitle')}
        </h2>

        <label className="mt-4 block text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.name')}</span>
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') submit();
            }}
            placeholder={t('team.namePlaceholder')}
            autoFocus
            className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
          />
        </label>

        <label className="mt-3 block text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.directory')}</span>
          <span className="mt-1 flex gap-2">
            <input
              value={directory}
              onChange={(event) => setDirectory(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') submit();
              }}
              placeholder={t('team.directoryPlaceholder')}
              className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 font-mono text-xs dark:border-neutral-700"
            />
            {bridge && (
              <button
                type="button"
                onClick={browse}
                title={t('team.browse')}
                aria-label={t('team.browse')}
                className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-neutral-300 text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900 dark:border-neutral-700 dark:hover:bg-neutral-800"
              >
                <FolderOpen size={16} aria-hidden="true" />
              </button>
            )}
            <button
              type="button"
              onClick={checkDirectory}
              disabled={checking || !directory.trim()}
              className="shrink-0 rounded-md border border-neutral-300 px-2 py-1.5 text-xs text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              {t('team.checkDirectory')}
            </button>
          </span>
        </label>

        {!bridge && (
          <div className="mt-1.5">
            <button
              type="button"
              onClick={listChildren}
              className="text-xs text-neutral-500 underline-offset-2 hover:underline dark:text-neutral-400"
            >
              {t('team.listChildren')}
            </button>
            {children.length > 0 && (
              <ul className="mt-1 max-h-28 overflow-y-auto rounded-md border border-neutral-200 dark:border-neutral-700">
                {children.map((child) => (
                  <li key={child}>
                    <button
                      type="button"
                      onClick={() => {
                        setDirectory(child);
                        setChildren([]);
                        setVerdict(null);
                      }}
                      className="block w-full truncate px-2 py-1 text-left font-mono text-[11px] hover:bg-neutral-100 dark:hover:bg-neutral-800"
                    >
                      {child}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        {verdict && (
          <p
            role="status"
            className={`mt-2 text-xs ${verdict.ok ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400'}`}
          >
            {verdict.ok
              ? t('team.directoryOk', { path: verdict.canonical })
              : verdict.duplicate
                ? t('team.directoryDuplicate', { teamName: verdict.teamName ?? '' })
                : verdict.nested
                  ? t('team.directoryNested', { teamName: verdict.teamName ?? '' })
                  : t('status.error')}
            {verdict.markerPresent && (
              <span className="block">
                {t('team.directoryMarker', { instanceId: verdict.markerInstance ?? '' })}
                <label className="mt-1 flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={adoptMarker}
                    onChange={() => setAdoptMarker((value) => !value)}
                    className="h-4 w-4 rounded border-neutral-300"
                  />
                  {t('team.adoptMarker')}
                </label>
              </span>
            )}
          </p>
        )}

        <fieldset className="mt-3">
          <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('team.initialRoles')}</legend>
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
          {roles.isSuccess && selectableRoles.length === 0 && (
            <p className="mt-1 text-xs text-neutral-400">{t('team.noSelectableRoles')}</p>
          )}
          <div className="mt-1 grid max-h-40 gap-1 overflow-y-auto sm:grid-cols-2">
            {selectableRoles.map((role) => (
              <label key={role.id} className="flex items-center gap-2 rounded px-1 py-0.5 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800">
                <input
                  type="checkbox"
                  checked={selectedRoles.has(role.id)}
                  onChange={() => toggleRole(role.id)}
                  className="h-4 w-4 rounded border-neutral-300"
                />
                <span className="min-w-0 truncate">{t(role.nameKey, { defaultValue: role.name })}</span>
              </label>
            ))}
          </div>
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
