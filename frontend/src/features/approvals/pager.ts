// Pure pager math for the one-card-at-a-time approval stack. Processing a
// card shrinks the list, so the index must stay inside bounds without ever
// auto-advancing (no autoplay by product rule).
import type { ApprovalOption } from '../../api/types';

/** Clamps a pager index into [0, count-1]; empty lists report 0. */
export function clampPagerIndex(index: number, count: number): number {
  if (!Number.isFinite(index) || count <= 0) return 0;
  return Math.min(Math.max(Math.trunc(index), 0), count - 1);
}

/** i18n key candidates for an option button label: the engine-provided
 * messageKey first, then the built-in approval.option.* key of the option
 * id, then the generic unknown-option label. */
export function optionLabelKeys(option: ApprovalOption): string[] {
  const builtin = BUILTIN_OPTION_KEYS[option.id];
  const keys: string[] = [];
  if (option.messageKey) keys.push(option.messageKey);
  if (builtin) keys.push(builtin);
  keys.push('approval.option.unknown');
  return keys;
}

/** Fallback text when no translation exists: the server-provided text, then
 * the raw option id — the label is never blank. */
export function optionFallbackText(option: ApprovalOption): string {
  return option.text ?? option.id;
}

const BUILTIN_OPTION_KEYS: Record<string, string> = {
  accept: 'approval.option.allow_once',
  acceptForSession: 'approval.option.allow_thread_session',
  decline: 'approval.option.decline',
  deny: 'approval.option.permissions.deny',
};

/** approvalSummaryText renders a stored summary. Product cards store an
 * i18n key (never prose in one language); engine summaries are model text
 * and pass through unchanged. */
export function approvalSummaryText(
  t: (key: string, opts?: Record<string, unknown>) => string,
  summary: string,
): string {
  return summary.startsWith('approval.') ? t(summary, { defaultValue: summary }) : summary;
}
