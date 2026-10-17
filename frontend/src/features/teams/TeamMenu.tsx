import { useEffect, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { MoreHorizontal } from 'lucide-react';
import { api } from '../../api/client';
import { useArchiveTeam, useDeleteBot, useDeleteTeam, useRoles } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import ConfirmDialog from '../../components/ConfirmDialog';
import { useDialogFocus } from '../../components/useDialogFocus';
import { useSelectedBot } from '../../app/SelectedBotContext';
import { apiErrorMessage } from '../../i18n/errors';

type Dialog = 'archive' | 'deleteTeam' | 'addMember' | 'deleteMember' | null;

const itemClass =
  'block w-full px-3 py-1.5 text-left text-sm hover:bg-neutral-100 disabled:opacity-50 dark:hover:bg-neutral-800';

/**
 * TeamMenu keeps the team list clean: every team action sits behind one
 * "more" button. What it offers follows the team's state — an active team
 * can add or delete members and be archived (only when idle; it then moves
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
    entries.push({ key: 'add', label: t('team.tools.add'), run: () => choose('addMember') });
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
      {dialog === 'addMember' && <AddMemberDialog item={item} onClose={() => setDialog(null)} />}
      {dialog === 'deleteMember' && <DeleteMemberDialog item={item} onClose={() => setDialog(null)} />}
    </>
  );
}

/** AddMemberDialog adds one member of a chosen role to an active team. */
function AddMemberDialog({ item, onClose }: { item: TeamWithBots; onClose: () => void }) {
  const { t } = useTranslation();
  const { team } = item;
  const qc = useQueryClient();
  const roles = useRoles(true);
  const [roleId, setRoleId] = useState('');
  const add = useMutation({
    mutationFn: () => api.addBot(team.id, roleId),
    onSuccess: () => {
      void qc.invalidateQueries();
      onClose();
    },
  });
  const dialogRef = useDialogFocus(true, onClose, add.isPending);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onMouseDown={() => !add.isPending && onClose()}>
      <form
        ref={dialogRef as React.RefObject<HTMLFormElement>}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`add-member-${team.id}`}
        onMouseDown={(event) => event.stopPropagation()}
        onSubmit={(event) => {
          event.preventDefault();
          if (roleId) add.mutate();
        }}
        className="w-full max-w-sm rounded-xl border border-neutral-200 bg-white p-5 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id={`add-member-${team.id}`} className="text-sm font-semibold">
          {t('team.tools.add')} · {team.name}
        </h2>
        <label className="mt-3 block text-sm">
          <span className="text-neutral-500">{t('team.roleLabel')}</span>
          <select
            value={roleId}
            onChange={(event) => setRoleId(event.target.value)}
            className="mt-1 w-full rounded-md border border-neutral-300 bg-transparent px-2 py-1.5 dark:border-neutral-700"
          >
            <option value="">{t('team.chooseRole')}</option>
            {roles.data?.items
              .filter((role) => role.id !== 'leader')
              .map((role) => (
                <option value={role.id} key={role.id}>
                  {t(role.nameKey, { defaultValue: role.name })}
                </option>
              ))}
          </select>
        </label>
        {roles.isError && (
          <p role="alert" className="mt-2 text-xs text-red-600">
            {t('team.loadFailed')}
          </p>
        )}
        {add.isError && (
          <p role="alert" className="mt-2 text-sm text-red-600">
            {apiErrorMessage(t, add.error)}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={add.isPending} className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100">
            {t('team.cancel')}
          </button>
          <button
            type="submit"
            disabled={add.isPending || !roleId}
            className="rounded-md bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
          >
            {add.isPending ? t('status.loading') : t('team.tools.add')}
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
