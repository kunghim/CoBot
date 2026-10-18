// Avatar shows a member: its own avatar, the one chosen for its role, or the
// role's default from the downloaded catalog, in the member's current state
// (moving while it works, wide eyes while it waits for you). A local picture
// shows its state as a colored ring. Until a catalog is cached the bundled
// fallback stands in.
import { useAvatarCatalog, useAvatarUpload, useRoles } from '../../api/queries';
import type { AvatarRef, Bot } from '../../api/types';
import { FALLBACK_AVATAR, avatarStateOf, drawAvatar, resolveAvatar, type AvatarState } from './render';

export const AVATAR_SIZES = { sm: 16, md: 24, lg: 36, xl: 48, xxl: 96 } as const;

const RING: Record<AvatarState, string> = {
  idle: '',
  working: 'ring-2 ring-emerald-500 animate-pulse',
  waiting: 'ring-2 ring-amber-500',
  stopped: 'ring-2 ring-red-400',
};

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
  const state = showState ? avatarStateOf(bot.status) : 'idle';
  const px = AVATAR_SIZES[size];
  if (resolved?.kind === 'upload') return <PictureAvatar file={resolved.file} size={px} state={state} />;
  const src = resolved ? drawAvatar(resolved.series, resolved.avatar, state) : FALLBACK_AVATAR;
  return <AvatarImage src={src} size={px} />;
}

function PictureAvatar({ file, size, state }: { file: string; size: number; state: AvatarState }) {
  const picture = useAvatarUpload(file);
  return <AvatarImage src={picture.data ?? FALLBACK_AVATAR} size={size} picture className={RING[state]} />;
}

export function AvatarImage({ src, size, picture = false, className = '' }: { src: string; size: number; picture?: boolean; className?: string }) {
  return (
    <img
      src={src}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
      draggable={false}
      data-avatar
      className={`shrink-0 select-none ${picture ? 'rounded-[28%] object-cover' : ''} ${className}`}
      style={{ width: size, height: size }}
    />
  );
}
