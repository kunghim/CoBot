import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Check, Lock, MoreHorizontal } from 'lucide-react';
import { api } from '../../api/client';
import { useArchiveTeam, useDeleteBot, useDeleteTeam, useRenameTeam, useRoles } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import ConfirmDialog from '../../components/ConfirmDialog';
import { useDialogFocus } from '../../components/useDialogFocus';
import { useSelectedBot } from '../../app/SelectedBotContext';
import { apiErrorMessage } from '../../i18n/errors';

type Dialog = 'archive' | 'deleteTeam' | 'edit' | 'deleteMember' | null;

const itemClass =
  'block w-full px-3 py-1.5 text-left text-sm hover:bg-neutral-100 disabled:opacity-50 dark:hover:bg-neutral-800';

/**
 * TeamMenu keeps the team list clean: every team action sits behind one
 * "more" button. What it offers follows the team's state — an active team
 * can be edited (name, new members), lose members and be archived (only when idle; it then moves
 * to Settings), and a team whose folder is gone can only be deleted. Each
 * action that changes the team asks first and shows a refusal inside its
 * dialog.
 */
export default function TeamMenu({ item }: { item: TeamWithBots }) {
  const { t } = useTranslation();
  const { team, bots } = item;
  const { botId, setBotId } = useSelectedBot();
  const archive = useArchiveTeam();
  const removeTeam = useDeleteTeam();
  const [open, setOpen] = useState(false);
  const [dialog, setDialog] = useState<Dialog>(null);
  const [errorText, setErrorText] = useState('');
  const menuRef = useRef<HTMLDivElement | null>(null);

  // The menu closes on any click outside it or on Escape.
  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  // Leaving the sidebar drops a selection inside the team.
  const leave = () => {
    if (botId && bots.some((bot) => bot.id === botId)) setBotId(null);
  };
  const choose = (next: Dialog) => {
    setOpen(false);
    setErrorText('');
    archive.reset();
    removeTeam.reset();
    setDialog(next);
  };

  const entries: { key: string; label: string; danger?: boolean; run: () => void }[] = [];
  if (team.status === 'active') {
    entries.push({ key: 'edit', label: t('team.edit'), run: () => choose('edit') });
    if (bots.some((bot) => !bot.isLeader)) {
      entries.push({ key: 'deleteMember', label: t('team.deleteMember'), run: () => choose('deleteMember') });
    }
    entries.push({ key: 'archive', label: t('team.archive'), run: () => choose('archive') });
  }
  if (team.status === 'directory_missing') {
    entries.push({ key: 'delete', label: t('team.delete'), danger: true, run: () => choose('deleteTeam') });
  }
  if (entries.length === 0) return null;

  return (
    <>
      {errorText && (
        <span role="alert" className="w-full text-[10px] leading-4 text-red-600 dark:text-red-400">
          {errorText}
        </span>
      )}
      <div ref={menuRef} className="relative shrink-0">
        <button
          type="button"
          onClick={() => setOpen((value) => !value)}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label={t('team.actions', { name: team.name })}
          title={t('team.actions', { name: team.name })}
          className="flex h-6 w-6 items-center justify-center rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
        >
          <MoreHorizontal size={14} aria-hidden="true" />
        </button>
        {open && (
          <div
            role="menu"
            className="absolute right-0 z-20 mt-1 w-36 overflow-hidden rounded-md border border-neutral-200 bg-white py-1 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
          >
            {entries.map((entry) => (
              <button
                key={entry.key}
                type="button"
                role="menuitem"
                onClick={entry.run}
                className={`${itemClass} ${entry.danger ? 'text-red-600' : ''}`}
              >
                {entry.label}
              </button>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={dialog === 'archive'}
        title={t('team.archiveConfirmTitle', { name: team.name })}
        body={t('team.archiveConfirmBody')}
        confirmLabel={t('team.archive')}
        danger={false}
        pending={archive.isPending}
        errorText={archive.isError ? apiErrorMessage(t, archive.error) : ''}
        onCancel={() => setDialog(null)}
        onConfirm={() =>
          archive.mutate(
            { teamId: team.id, expectedRevision: team.revision },
            {
              onSuccess: () => {
                setDialog(null);
                leave();
              },
            },
          )
        }
      />
      <ConfirmDialog
        open={dialog === 'deleteTeam'}
        title={t('team.deleteConfirmTitle', { name: team.name })}
        body={t('team.deleteMissingConfirmBody')}
        confirmLabel={t('team.delete')}
        pending={removeTeam.isPending}
        errorText={removeTeam.isError ? apiErrorMessage(t, removeTeam.error) : ''}
        onCancel={() => setDialog(null)}
        onConfirm={() =>
          removeTeam.mutate(
            { teamId: team.id, expectedRevision: team.revision },
            {
              onSuccess: () => {
                setDialog(null);
                leave();
              },
            },
          )
        }
      />
      {dialog === 'edit' && <EditTeamDialog item={item} onClose={() => setDialog(null)} />}
      {dialog === 'deleteMember' && <DeleteMemberDialog item={item} onClose={() => setDialog(null)} />}
    </>
  );
}

/**
 * EditTeamDialog edits what may change after creation: the team name, and
 * new members added from the team's roles. Existing members are shown locked
 * (they cannot be changed here) and the folder is shown read-only, because a
 * team keeps its folder for life.
 */
function EditTeamDialog({ item, onClose }: { item: TeamWithBots; onClose: () => void }) {
  const { t } = useTranslation();
  const { team, bots } = item;
  const qc = useQueryClient();
  const roles = useRoles(true);
  const rename = useRenameTeam();
  const [name, setName] = useState(team.name);
  const [adding, setAdding] = useState<ReadonlySet<string>>(() => new Set());
  const [saving, setSaving] = useState(false);
  const [errorText, setErrorText] = useState('');
  const dialogRef = useDialogFocus(true, onClose, saving);

  const library = roles.data?.items ?? [];
  const present = new Set(bots.map((bot) => bot.roleId));
  const scenario = roles.data?.scenarios?.[0];
  const offered = (scenario ? scenario.roleIds : library.filter((role) => role.id !== 'leader').map((role) => role.id))
    .filter((id) => !present.has(id))
    .map((id) => library.find((role) => role.id === id))
    .filter((role) => role !== undefined);
  const summaryOf = (roleId: string) => {
    const role = library.find((entry) => entry.id === roleId);
    return role?.summaryKey ? t(role.summaryKey, { defaultValue: '' }) : '';
  };
  const trimmed = name.trim();
  const changed = trimmed !== team.name || adding.size > 0;

  const save = async () => {
    if (!trimmed || !changed) return;
    setSaving(true);
    setErrorText('');
    try {
      if (trimmed !== team.name) {
        await rename.mutateAsync({ teamId: team.id, name: trimmed, expectedRevision: team.revision });
      }
      for (const roleId of adding) {
        await api.addBot(team.id, roleId);
        setAdding((prev) => {
          const next = new Set(prev);
          next.delete(roleId);
          return next;
        });
      }
      void qc.invalidateQueries();
      onClose();
    } catch (error) {
      // Members added before a failure stay added; the dialog keeps what is
      // left so it can be saved again.
      void qc.invalidateQueries();
      setErrorText(apiErrorMessage(t, error));
    } finally {
      setSaving(false);
    }
  };

  const card = (key: string, label: string, summary: string, selected: boolean, locked: boolean, onToggle?: () => void) => (
    <button
      key={key}
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-disabled={locked}
      onClick={() => {
        if (!locked && onToggle) onToggle();
      }}
      className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-left ${
        selected
          ? 'border-neutral-900 bg-neutral-50 dark:border-neutral-200 dark:bg-neutral-800'
          : 'border-neutral-200 hover:border-neutral-400 dark:border-neutral-700 dark:hover:border-neutral-500'
      } ${locked ? 'cursor-default opacity-70' : ''}`}
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
      <span className="min-w-0">
        <span className="block text-sm font-medium">{label}</span>
        {summary && <span className="mt-0.5 block text-xs text-neutral-500 dark:text-neutral-400">{summary}</span>}
      </span>
    </button>
  );

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onMouseDown={() => !saving && onClose()}>
      <form
        ref={dialogRef as React.RefObject<HTMLFormElement>}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`edit-team-${team.id}`}
        onMouseDown={(event) => event.stopPropagation()}
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
        className="max-h-full w-full max-w-xl overflow-y-auto rounded-xl border border-neutral-200 bg-white p-5 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id={`edit-team-${team.id}`} className="text-base font-semibold">
          {t('team.edit')}
        </h2>

        <div className="mt-4 text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.directory')}</span>
          <p className="mt-1 break-all rounded-md bg-neutral-100 px-3 py-1.5 font-mono text-xs text-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">
            {team.directory}
          </p>
          <p className="mt-1 text-xs text-neutral-400">{t('team.directoryFixed')}</p>
        </div>

        <label className="mt-4 block text-sm">
          <span className="text-neutral-500 dark:text-neutral-400">{t('team.name')}</span>
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
          />
        </label>

        <fieldset className="mt-4">
          <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('team.members')}</legend>
          <div className="mt-1 grid gap-2 sm:grid-cols-2">
            {bots.map((bot) => card(bot.id, bot.name, summaryOf(bot.roleId), true, true))}
            {offered.map((role) =>
              card(
                role.id,
                t(role.nameKey, { defaultValue: role.name }),
                role.summaryKey ? t(role.summaryKey, { defaultValue: '' }) : '',
                adding.has(role.id),
                false,
                () =>
                  setAdding((prev) => {
                    const next = new Set(prev);
                    if (next.has(role.id)) next.delete(role.id);
                    else next.add(role.id);
                    return next;
                  }),
              ),
            )}
          </div>
          <p className="mt-1 text-xs text-neutral-400">{t('team.membersLocked')}</p>
        </fieldset>

        {errorText && (
          <p className="mt-3 text-sm text-red-600" role="alert">
            {errorText}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={saving}
            className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 disabled:opacity-50 dark:hover:text-neutral-100"
          >
            {t('team.cancel')}
          </button>
          <button
            type="submit"
            disabled={saving || !trimmed || !changed}
            className="rounded-md bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {saving ? t('status.loading') : t('team.save')}
          </button>
        </div>
      </form>
    </div>
  );
}

/**
 * DeleteMemberDialog picks one member (never the leader) and deletes it
 * after showing what goes with it. A refusal (unfinished work) is shown in
 * the dialog with every blocker named.
 */
function DeleteMemberDialog({ item, onClose }: { item: TeamWithBots; onClose: () => void }) {
  const { t } = useTranslation();
  const { team, bots } = item;
  const { botId, setBotId } = useSelectedBot();
  const remove = useDeleteBot();
  const members = bots.filter((bot) => !bot.isLeader);
  const [chosen, setChosen] = useState('');
  const dialogRef = useDialogFocus(true, onClose, remove.isPending);
  const target = members.find((bot) => bot.id === chosen);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onMouseDown={() => !remove.isPending && onClose()}>
      <div
        ref={dialogRef as React.RefObject<HTMLDivElement>}
        tabIndex={-1}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={`delete-member-${team.id}`}
        onMouseDown={(event) => event.stopPropagation()}
        className="w-full max-w-sm rounded-xl border border-neutral-200 bg-white p-5 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id={`delete-member-${team.id}`} className="text-sm font-semibold">
          {t('team.deleteMember')} · {team.name}
        </h2>
        <fieldset className="mt-3 space-y-1">
          <legend className="text-sm text-neutral-500">{t('team.deleteMemberPick')}</legend>
          {members.map((bot) => (
            <label key={bot.id} className="flex items-center gap-2 rounded px-1 py-1 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800">
              <input
                type="radio"
                name={`delete-member-${team.id}`}
                checked={chosen === bot.id}
                onChange={() => {
                  remove.reset();
                  setChosen(bot.id);
                }}
              />
              {bot.name}
            </label>
          ))}
        </fieldset>
        {target && <p className="mt-3 text-sm text-neutral-600 dark:text-neutral-300">{t('team.deleteMemberConfirmBody')}</p>}
        {remove.isError && (
          <p role="alert" className="mt-3 text-sm text-red-600">
            {apiErrorMessage(t, remove.error)}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={remove.isPending} className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 disabled:opacity-50 dark:hover:text-neutral-100">
            {t('team.cancel')}
          </button>
          <button
            type="button"
            disabled={!target || remove.isPending}
            onClick={() => {
              if (!target) return;
              remove.mutate(
                { botId: target.id, expectedRevision: target.revision },
                {
                  onSuccess: () => {
                    if (botId === target.id) setBotId(null);
                    onClose();
                  },
                },
              );
            }}
            className="rounded-md bg-red-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
          >
            {remove.isPending ? t('status.loading') : t('team.deleteMember')}
          </button>
        </div>
      </div>
    </div>
  );
}
