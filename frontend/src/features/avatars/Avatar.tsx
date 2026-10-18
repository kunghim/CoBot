// Avatar shows a member: its role's avatar from the downloaded catalog, in
// the member's current state (moving while it works, wide eyes while it waits
// for you). Until a catalog is cached it shows the bundled fallback.
import { useAvatarCatalog } from '../../api/queries';
import type { Bot } from '../../api/types';
import { FALLBACK_AVATAR, avatarStateOf, defaultSeries, drawAvatar, roleAvatar } from './render';

const SIZES = { sm: 16, md: 24, lg: 36, xl: 48 } as const;

export default function Avatar({
  bot,
  size = 'md',
  showState = false,
}: {
  bot: Pick<Bot, 'roleId' | 'isLeader'> & Partial<Pick<Bot, 'status'>>;
  size?: keyof typeof SIZES;
  /** Draw the member's state; lists and headers do, pickers do not. */
  showState?: boolean;
}) {
  const catalog = useAvatarCatalog();
  const series = defaultSeries(catalog.data);
  const roleId = bot.isLeader ? 'leader' : bot.roleId;
  const avatar = series ? roleAvatar(series, roleId) : null;
  const src = series && avatar ? drawAvatar(series, avatar, showState ? avatarStateOf(bot.status) : 'idle') : FALLBACK_AVATAR;
  const px = SIZES[size];
  return (
    <img
      src={src}
      width={px}
      height={px}
      alt=""
      aria-hidden="true"
      draggable={false}
      data-avatar
      className="shrink-0 select-none"
      style={{ width: px, height: px }}
    />
  );
}
