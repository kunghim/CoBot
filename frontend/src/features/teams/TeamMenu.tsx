import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import { useArchiveTeam, useDeleteBot, useDeleteTeam, useRenameTeam, useRoles } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import { useSelectedBot } from '../../app/SelectedBotContext';
import { Alert, AlertDialog, Button, Checkbox, DescriptionList, Dialog, Field, Fieldset, IconButton, Icons, Menu, RadioGroup, TextInput, type MenuItem } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import TeamBudgetDialog from './TeamBudgetDialog';

type DialogId = 'archive' | 'deleteTeam' | 'edit' | 'budget' | 'deleteMember' | null;

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
  const [dialog, setDialog] = useState<DialogId>(null);

  // Leaving the sidebar drops a selection inside the team.
  const leave = () => {
    if (botId && bots.some((bot) => bot.id === botId)) setBotId(null);
  };
  const choose = (next: DialogId) => {
    archive.reset();
    removeTeam.reset();
    setDialog(next);
  };

  const entries: MenuItem[] = [];
  if (team.status === 'active') {
    entries.push({ id: 'edit', label: t('team.edit'), icon: Icons.edit, onSelect: () => choose('edit') });
    entries.push({ id: 'budget', label: t('team.budget'), icon: Icons.model, onSelect: () => choose('budget') });
    if (bots.some((bot) => !bot.isLeader)) {
      entries.push({ id: 'deleteMember', label: t('team.deleteMember'), icon: Icons.delete, onSelect: () => choose('deleteMember') });
    }
    entries.push({ id: 'archive', label: t('team.archive'), icon: Icons.archive, onSelect: () => choose('archive') });
  }
  if (team.status === 'directory_missing') {
    entries.push({ id: 'delete', label: t('team.delete'), icon: Icons.delete, danger: true, onSelect: () => choose('deleteTeam') });
  }
  if (entries.length === 0) return null;

  return (
    <>
      <Menu label={t('team.actions', { name: team.name })} items={entries} trigger={<IconButton icon={Icons.more} label={t('team.actions', { name: team.name })} size="sm" />} />

      <AlertDialog
        open={dialog === 'archive'}
        title={t('team.archiveConfirmTitle', { name: team.name })}
        description={t('team.archiveConfirmBody')}
        confirmLabel={t('team.archive')}
        cancelLabel={t('common.cancel')}
        danger={false}
        busy={archive.isPending}
        error={archive.isError ? apiErrorMessage(t, archive.error) : undefined}
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
      <AlertDialog
        open={dialog === 'deleteTeam'}
        title={t('team.deleteConfirmTitle', { name: team.name })}
        description={t('team.deleteMissingConfirmBody')}
        confirmLabel={t('team.delete')}
        cancelLabel={t('common.cancel')}
        busy={removeTeam.isPending}
        error={removeTeam.isError ? apiErrorMessage(t, removeTeam.error) : undefined}
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
      {dialog === 'budget' && <TeamBudgetDialog item={item} onClose={() => setDialog(null)} />}
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
    if (!trimmed || !changed || saving) return;
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

  const toggle = (roleId: string) =>
    setAdding((prev) => {
      const next = new Set(prev);
      if (next.has(roleId)) next.delete(roleId);
      else next.add(roleId);
      return next;
    });

  return (
    <Dialog
      open
      size="lg"
      onClose={onClose}
      busy={saving}
      closeLabel={t('common.close')}
      title={t('team.edit')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" onClick={() => void save()} loading={saving} disabled={!trimmed || !changed}>
            {t('team.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        <DescriptionList items={[{ term: t('team.directory'), value: team.directory, mono: true }]} />
        <p className="-mt-3 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('team.directoryFixed')}</p>
        <Field label={t('team.name')}>
          <TextInput
            value={name}
            onChange={(event) => setName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') void save();
            }}
          />
        </Field>
        <Fieldset legend={t('team.members')} description={t('team.membersLocked')}>
          <div className="grid gap-3 sm:grid-cols-2">
            {bots.map((bot) => (
              <Checkbox key={bot.id} checked disabled onChange={() => {}} label={bot.name} description={summaryOf(bot.roleId)} />
            ))}
            {offered.map((role) => (
              <Checkbox
                key={role.id}
                checked={adding.has(role.id)}
                onChange={() => toggle(role.id)}
                label={t(role.nameKey, { defaultValue: role.name })}
                description={role.summaryKey ? t(role.summaryKey, { defaultValue: '' }) : undefined}
              />
            ))}
          </div>
        </Fieldset>
        {errorText && <Alert tone="danger">{errorText}</Alert>}
      </div>
    </Dialog>
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
  const target = members.find((bot) => bot.id === chosen);
  return (
    <AlertDialog
      open
      title={`${t('team.deleteMember')} · ${team.name}`}
      description={target ? t('team.deleteMemberConfirmBody') : undefined}
      confirmLabel={t('team.deleteMember')}
      cancelLabel={t('common.cancel')}
      busy={remove.isPending}
      confirmDisabled={!target}
      error={remove.isError ? apiErrorMessage(t, remove.error) : undefined}
      onCancel={onClose}
      onConfirm={() => {
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
    >
      <Fieldset legend={t('team.deleteMemberPick')}>
      <RadioGroup
        label={t('team.deleteMemberPick')}
        value={chosen}
        onChange={(id) => {
          remove.reset();
          setChosen(id);
        }}
        options={members.map((bot) => ({ value: bot.id, label: bot.name }))}
      />
      </Fieldset>
    </AlertDialog>
  );
}
