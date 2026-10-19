// The two places an avatar is chosen: a member's own (from its conversation
// header) and a role's (from Settings > Roles). Both open the same picker.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAvatarCatalog, useRoles, useSetBotAvatar, useSetRoleAvatar } from '../../api/queries';
import type { AvatarRef, Bot, Role } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { Button, ListRow } from '../../components/ui';
import Avatar from './Avatar';
import AvatarPicker from './AvatarPicker';
import { memberStartPicture, pictureOf } from './choice';
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
        // The role's picture shows only while the member has no avatar of its
        // own; a member's own composed avatar must never open as the role's.
        currentPicture={memberStartPicture(bot.avatar, roleChoice)}
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
    <ListRow
      leading={<Avatar bot={{ roleId: role.id, isLeader }} size="lg" />}
      title={name}
      description={role.avatar ? t('avatar.custom') : t('avatar.default')}
      trailing={
        <>
          <Button
            size="sm"
            onClick={() => {
              setAvatar.reset();
              setOpen(true);
            }}
          >
            {t('avatar.change')}
          </Button>
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
        </>
      }
    />
  );
}

