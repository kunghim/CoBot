// Every member wears its role's color wherever it appears (team list,
// conversation header, exchange lines, task owners), so who did what can be
// told apart at a glance. Built-in roles have fixed colors; any other role
// gets a stable one from the same set by its id.
import type { Bot } from '../../api/types';

export interface RoleTone {
  /** Solid mark: initial tiles and the selection bar. */
  solid: string;
  /** Soft chip: name badges on light and dark backgrounds. */
  chip: string;
}

const TONES: Record<string, RoleTone> = {
  leader: {
    solid: 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900',
    chip: 'bg-neutral-200 text-neutral-800 dark:bg-neutral-700 dark:text-neutral-100',
  },
  'product-manager': {
    solid: 'bg-[#8c3fa3] text-white dark:bg-[#c58ad6] dark:text-[#2a1131]',
    chip: 'bg-[#f3e6f7] text-[#6b2c7d] dark:bg-[#3a2242] dark:text-[#e3c4ec]',
  },
  'fullstack-engineer': {
    solid: 'bg-[#2463c9] text-white dark:bg-[#7fa9f0] dark:text-[#0d1f3d]',
    chip: 'bg-[#e3ecfb] text-[#1b4c9b] dark:bg-[#1c2a45] dark:text-[#bcd2f7]',
  },
  // Unused by built-in roles; it keeps custom roles in five distinct colors.
  teal: {
    solid: 'bg-[#0f7f72] text-white dark:bg-[#5cc4b6] dark:text-[#082a26]',
    chip: 'bg-[#dcf1ee] text-[#0b5e54] dark:bg-[#15332f] dark:text-[#a6e3da]',
  },
  'qa-engineer': {
    solid: 'bg-[#c4502f] text-white dark:bg-[#ef9a7e] dark:text-[#3a140a]',
    chip: 'bg-[#fbe8e1] text-[#97391f] dark:bg-[#3f2119] dark:text-[#f5c3b2]',
  },
  'code-reviewer': {
    solid: 'bg-[#a17a12] text-white dark:bg-[#e2c062] dark:text-[#2e2304]',
    chip: 'bg-[#f6efd9] text-[#7a5c0c] dark:bg-[#3a3016] dark:text-[#ecd9a2]',
  },
};

const EXTRA = ['product-manager', 'fullstack-engineer', 'teal', 'qa-engineer', 'code-reviewer'];

export function roleTone(roleId: string | undefined, isLeader = false): RoleTone {
  if (isLeader) return TONES.leader;
  if (roleId && TONES[roleId]) return TONES[roleId];
  let hash = 0;
  for (const char of roleId ?? '') hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return TONES[EXTRA[Math.abs(hash) % EXTRA.length]];
}

/** The square initial tile that stands for a member. */
export function MemberMark({ bot, size = 'md' }: { bot: Pick<Bot, 'name' | 'roleId' | 'isLeader'>; size?: 'sm' | 'md' | 'lg' }) {
  const box = size === 'lg' ? 'h-9 w-9 rounded-lg text-sm' : size === 'sm' ? 'h-4 w-4 rounded text-[9px]' : 'h-6 w-6 rounded-md text-[11px]';
  return (
    <span
      aria-hidden="true"
      className={`inline-flex shrink-0 select-none items-center justify-center font-semibold leading-none ${box} ${roleTone(bot.roleId, bot.isLeader).solid}`}
    >
      {Array.from(bot.name.trim())[0] ?? '·'}
    </span>
  );
}
