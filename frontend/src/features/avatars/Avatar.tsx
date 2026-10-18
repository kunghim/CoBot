// Avatar shows a member: its own avatar, the one chosen for its role, or the
// role's default from the downloaded catalog, in the member's current state
// (moving while it works, wide eyes while it waits for you). Until a catalog
// is cached it shows the bundled fallback.
import { useAvatarCatalog, useRoles } from '../../api/queries';
import type { AvatarRef, Bot } from '../../api/types';
import { FALLBACK_AVATAR, avatarStateOf, drawAvatar, resolveAvatar } from './render';

export const AVATAR_SIZES = { sm: 16, md: 24, lg: 36, xl: 48, xxl: 96 } as const;

export default function Avatar({
  bot,
  size = 'md',
  showState = false,
}: {
  bot: Pick<Bot, 'roleId' | 'isLeader'> & Partial<Pick<Bot, 'status'>> & { avatar?: AvatarRef | null };
  size?: keyof typeof AVATAR_SIZES;
  /** Draw the member's state; lists and headers do, pickers do not. */
  showState?: boolean;
}) {
  const catalog = useAvatarCatalog();
  const roles = useRoles();
  const roleId = bot.isLeader ? 'leader' : bot.roleId;
  const roleChoice = roles.data?.items.find((role) => role.id === roleId)?.avatar;
  const resolved = resolveAvatar(catalog.data, roleId, bot.avatar, roleChoice);
  const src = resolved ? drawAvatar(resolved.series, resolved.avatar, showState ? avatarStateOf(bot.status) : 'idle') : FALLBACK_AVATAR;
  return <AvatarImage src={src} size={AVATAR_SIZES[size]} />;
}

export function AvatarImage({ src, size }: { src: string; size: number }) {
  return (
    <img
      src={src}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
      draggable={false}
      data-avatar
      className="shrink-0 select-none"
      style={{ width: size, height: size }}
    />
  );
}