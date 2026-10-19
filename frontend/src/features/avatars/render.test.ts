/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import type { AvatarCatalog, AvatarSeries } from '../../api/types';
import { FALLBACK_AVATAR, avatarStateOf, composedStart, defaultSeries, drawAvatar, resolveAvatar, roleAvatar } from './render';

// The series file the repository publishes, as the core serves it (tests run
// from frontend/).
const published = JSON.parse(readFileSync(resolve(process.cwd(), '../assets/avatars/dango/dist/dango.json'), 'utf8'));
const dango: AvatarSeries = { id: 'dango', name: { 'zh-CN': '团子' }, kind: 'composed', version: published.version, data: published };

describe('avatars', () => {
  it('uses the role default, and a stable pick for other roles', () => {
    expect(roleAvatar(dango, 'leader')).toEqual({ shape: 'star', eyes: 'shine', color: '#7d8c85' });
    const custom = roleAvatar(dango, 'data-analyst');
    expect(custom).toEqual(roleAvatar(dango, 'data-analyst'));
    expect(published.parts.shapes).toContain(custom?.shape);
    expect(published.parts.palette).toContain(custom?.color);
  });

  it('draws avatars as SVG data URIs, per state', () => {
    const leader = roleAvatar(dango, 'leader')!;
    const idle = drawAvatar(dango, leader, 'idle');
    expect(idle).toMatch(/^data:image\/svg\+xml/);
    expect(drawAvatar(dango, leader, 'waiting')).not.toBe(idle);
  });

  it('falls back when the catalog cannot draw', () => {
    expect(drawAvatar(dango, { shape: 'no-such-shape', eyes: 'dots', color: '#000000' })).toBe(FALLBACK_AVATAR);
    const unknown: AvatarCatalog = { series: [{ ...dango, kind: 'images' }], status: { syncing: false } };
    expect(defaultSeries(unknown)).toBeNull();
    expect(defaultSeries(undefined)).toBeNull();
  });

  it('maps member states', () => {
    expect(avatarStateOf('working')).toBe('working');
    expect(avatarStateOf('waiting_approval')).toBe('waiting');
    expect(avatarStateOf('waiting_input')).toBe('waiting');
    expect(avatarStateOf('interrupted')).toBe('stopped');
    expect(avatarStateOf('idle')).toBe('idle');
  });

  it('resolves a member own choice, then its role, then the default', () => {
    const catalog: AvatarCatalog = { series: [dango], status: { syncing: false } };
    const picture = { kind: 'upload' as const, file: 'a'.repeat(64) + '.png' };
    const flower = { kind: 'catalog' as const, series: 'dango', shape: 'flower', eyes: 'grin', color: '#f98b8b' };
    expect(resolveAvatar(catalog, 'leader', picture, flower)).toEqual({ kind: 'upload', file: picture.file });
    expect(resolveAvatar(catalog, 'leader', null, flower)).toMatchObject({ kind: 'composed', avatar: { shape: 'flower' } });
    expect(resolveAvatar(catalog, 'leader')).toMatchObject({ kind: 'composed', avatar: { shape: 'star' } });
    // A part the catalog no longer has falls through to the next choice.
    expect(resolveAvatar(catalog, 'leader', { ...flower, shape: 'gone' })).toMatchObject({ avatar: { shape: 'star' } });
    // Composing starts from a composed avatar even when a picture is shown.
    expect(composedStart(catalog, 'leader', picture, flower)).toMatchObject({ avatar: { shape: 'flower' } });
    // A picture shows even before any catalog is downloaded.
    expect(resolveAvatar(undefined, 'leader', picture)).toEqual({ kind: 'upload', file: picture.file });
    expect(resolveAvatar(undefined, 'leader')).toBeNull();
  });
});