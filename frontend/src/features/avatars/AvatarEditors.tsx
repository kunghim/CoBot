// The two places an avatar is chosen: a member's own (from its conversation
// header) and a role's (from Settings > Roles). Both open the same picker.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAvatarCatalog, useRoles, useSetBotAvatar, useSetRoleAvatar } from '../../api/queries';
import type { AvatarRef, Bot, Role } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import Avatar from './Avatar';
import AvatarPicker from './AvatarPicker';
import { composedStart } from './render';

/** A member's avatar that opens the picker for the member's own avatar. */
export function MemberAvatarButton({ bot, disabled = false }: { bot: Bot; disabled?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const catalog = useAvatarCatalog();
  const roles = useRoles();
  const setAvatar = useSetBotAvatar();
  const roleId = bot.isLeader ? 'leader' : bot.roleId;
  const roleChoice = roles.data?.items.find((role) => role.id === roleId)?.avatar;
  const save = (avatar: AvatarRef | null) =>
    setAvatar.mutate({ botId: bot.id, avatar, expectedRevision: bot.revision }, { onSuccess: () => setOpen(false) });
  return (
    <>
      <button
        type="button"
        onClick={() => {
          setAvatar.reset();
          setOpen(true);
        }}
        disabled={disabled}
        title={t('avatar.change')}
        className="shrink-0 rounded-lg transition-transform hover:scale-105 disabled:hover:scale-100"
      >
        <Avatar bot={bot} size="lg" showState />
      </button>
      <AvatarPicker
        open={open}
        title={t('avatar.memberTitle', { name: bot.name })}
        start={composedStart(catalog.data, roleId, bot.avatar, roleChoice)}
        currentPicture={pictureOf(bot.avatar) ?? pictureOf(roleChoice)}
        hasOwn={!!bot.avatar}
        pending={setAvatar.isPending}
        errorText={setAvatar.isError ? apiErrorMessage(t, setAvatar.error) : ''}
        onSave={save}
        onClose={() => setOpen(false)}
      />
    </>
  );
}

/** One role of the library with its avatar and a way to change it. */
export function RoleAvatarRow({ role }: { role: Role }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const catalog = useAvatarCatalog();
  const setAvatar = useSetRoleAvatar();
  const isLeader = role.id === 'leader';
  const name = t(role.nameKey, { defaultValue: role.name });
  const save = (avatar: AvatarRef | null) => setAvatar.mutate({ roleId: role.id, avatar }, { onSuccess: () => setOpen(false) });
  return (
    <li className="flex items-center gap-3 py-2.5">
      <Avatar bot={{ roleId: role.id, isLeader }} size="lg" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{name}</p>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">{role.avatar ? t('avatar.custom') : t('avatar.default')}</p>
      </div>
      <button
        type="button"
        onClick={() => {
          setAvatar.reset();
          setOpen(true);
        }}
        className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
      >
        {t('avatar.change')}
      </button>
      <AvatarPicker
        open={open}
        title={t('avatar.roleTitle', { name })}
        start={composedStart(catalog.data, role.id, null, role.avatar)}
        currentPicture={pictureOf(role.avatar)}
        hasOwn={!!role.avatar}
        pending={setAvatar.isPending}
        errorText={setAvatar.isError ? apiErrorMessage(t, setAvatar.error) : ''}
        onSave={save}
        onClose={() => setOpen(false)}
      />
    </li>
  );
}

function pictureOf(ref: AvatarRef | null | undefined): string | undefined {
  return ref?.kind === 'upload' ? ref.file : undefined;
}
