// A member's name badge (exchange lines, task owners) wears its role's color,
// the same hue as its avatar, so who did what can be told apart at a glance.
// Built-in roles have fixed colors; any other role gets a stable one from the
// same set by its id.
const CHIPS: Record<string, string> = {
  leader: 'bg-neutral-200 text-neutral-800 dark:bg-neutral-700 dark:text-neutral-100',
  'product-manager': 'bg-[#f3e6f7] text-[#6b2c7d] dark:bg-[#3a2242] dark:text-[#e3c4ec]',
  'frontend-dev': 'bg-[#e3ecfb] text-[#1b4c9b] dark:bg-[#1c2a45] dark:text-[#bcd2f7]',
  'backend-dev': 'bg-[#dcf1ee] text-[#0b5e54] dark:bg-[#15332f] dark:text-[#a6e3da]',
  'qa-engineer': 'bg-[#fbe8e1] text-[#97391f] dark:bg-[#3f2119] dark:text-[#f5c3b2]',
  'code-reviewer': 'bg-[#f6efd9] text-[#7a5c0c] dark:bg-[#3a3016] dark:text-[#ecd9a2]',
};

const EXTRA = ['product-manager', 'frontend-dev', 'backend-dev', 'qa-engineer', 'code-reviewer'];

/** The badge classes of a role. */
export function roleChip(roleId: string | undefined, isLeader = false): string {
  if (isLeader) return CHIPS.leader;
  if (roleId && CHIPS[roleId]) return CHIPS[roleId];
  let hash = 0;
  for (const char of roleId ?? '') hash = (hash * 31 + char.charCodeAt(0)) | 0;
  return CHIPS[EXTRA[Math.abs(hash) % EXTRA.length]];
}
