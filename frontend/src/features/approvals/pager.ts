// What an approval card reads: its option buttons and its stored summary.
import type { ApprovalOption } from '../../api/types';

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
  acceptForSession: 'approval.option.allow_session',
  decline: 'approval.option.decline',
  cancel: 'approval.option.decline_stop',
  grant: 'approval.option.grant_turn',
  grantForSession: 'approval.option.grant_session',
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
