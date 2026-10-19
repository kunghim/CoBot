// What an avatar editor opens with, and whether saving it would change
// anything. Kept apart from the components so the rules are tested alone.
import type { AvatarRef } from '../../api/types';
import type { ComposedAvatar } from './render';

/** The local picture a reference points at, if it is one. */
export function pictureOf(ref: AvatarRef | null | undefined): string | undefined {
  return ref?.kind === 'upload' ? ref.file : undefined;
}

/**
 * The picture a member's editor opens with: the member's own when it has
 * one; the role's only while the member has no avatar of its own. A member's
 * own composed avatar never opens as the role's picture.
 */
export function memberStartPicture(own: AvatarRef | null | undefined, role: AvatarRef | null | undefined): string | undefined {
  return own ? pictureOf(own) : pictureOf(role);
}

export type PickerChoice = { kind: 'stored'; file: string } | { kind: 'new' } | null;

const sameAvatar = (a: ComposedAvatar, b: ComposedAvatar) => a.shape === b.shape && a.eyes === b.eyes && a.color === b.color;

/**
 * Whether the picker still shows what it opened with. Saving then only
 * closes, so the avatar shown (which may be the role's) never becomes the
 * member's own by accident.
 */
export function unchangedChoice(choice: PickerChoice, startPicture: string | undefined, draft: ComposedAvatar | null, start: ComposedAvatar | null): boolean {
  if (choice === null) return !startPicture && !!draft && !!start && sameAvatar(draft, start);
  return choice.kind === 'stored' && choice.file === startPicture;
}
