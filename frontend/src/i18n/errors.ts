// Surfaces backend error messageKeys through i18n. Keys are stored under the
// "errors" translation block (literal dotted names); anything untranslated
// falls back to the raw key so the user always sees the server's discriminator
// instead of a blank string.
import type { TFunction } from 'i18next';
import type { ApiError } from '../api/client';

export function errorMessage(
  t: TFunction,
  messageKey: string,
  params: Record<string, unknown> = {},
): string {
  return t([`errors.${messageKey}`, messageKey], { defaultValue: messageKey, ...params });
}

/** One call for a caught ApiError; non-Api errors map to error.internal. */
export function apiErrorMessage(t: TFunction, error: unknown): string {
  if (error && typeof error === 'object' && 'messageKey' in error) {
    const apiError = error as ApiError;
    return errorMessage(t, apiError.messageKey, apiError.params ?? {});
  }
  return errorMessage(t, 'error.internal');
}
