import { describe, expect, it } from 'vitest';
import { memberStartPicture, unchangedChoice } from './choice';

const composed = { kind: 'composed', series: 'dango', version: 2, shape: 'cloud', eyes: 'happy', color: '#d8b1fb' } as const;
const rolePicture = { kind: 'upload', file: 'role.png' } as const;
const memberPicture = { kind: 'upload', file: 'member.png' } as const;
const dango = { shape: 'cloud', eyes: 'happy', color: '#d8b1fb' };

describe('memberStartPicture', () => {
  it('never opens a member with its own composed avatar on the role picture', () => {
    expect(memberStartPicture(composed as never, rolePicture as never)).toBeUndefined();
  });
  it('opens on the member picture when it has one', () => {
    expect(memberStartPicture(memberPicture as never, rolePicture as never)).toBe('member.png');
  });
  it('shows the role picture only while the member has no avatar of its own', () => {
    expect(memberStartPicture(undefined, rolePicture as never)).toBe('role.png');
    expect(memberStartPicture(null, undefined)).toBeUndefined();
  });
});

describe('unchangedChoice', () => {
  it('treats the role picture it opened with as unchanged, so saving does not copy it', () => {
    expect(unchangedChoice({ kind: 'stored', file: 'role.png' }, 'role.png', dango, dango)).toBe(true);
  });
  it('treats the composed avatar it opened with as unchanged', () => {
    expect(unchangedChoice(null, undefined, dango, dango)).toBe(true);
  });
  it('sees a new shape, a new picture, or leaving a picture for a composed avatar as a change', () => {
    expect(unchangedChoice(null, undefined, { ...dango, shape: 'star' }, dango)).toBe(false);
    expect(unchangedChoice({ kind: 'new' }, 'role.png', dango, dango)).toBe(false);
    expect(unchangedChoice(null, 'role.png', dango, dango)).toBe(false);
  });
});
