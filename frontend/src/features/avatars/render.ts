// Draws members' avatars from the downloaded catalog. A composed series (the
// dango series) is a DiceBear definition plus the parts the picker offers;
// an avatar is a choice of shape, eyes and color. Avatars are always shown as
// <img> data URIs, never inserted as markup, so a catalog file can only ever
// draw a picture.
import { Avatar, Style } from '@dicebear/core';
import type { AvatarCatalog, AvatarSeries, Bot } from '../../api/types';
import fallbackUrl from './fallback.svg';

export interface ComposedAvatar {
  shape: string;
  eyes: string;
  color: string;
}

type StateChange = { eyes?: string; animation?: string };

/** What a composed series file holds (see assets/avatars). */
interface ComposedSeriesData {
  schemaVersion: number;
  id: string;
  version: number;
  render?: Record<string, number>;
  parts: { shapes: string[]; eyes: string[]; palette: string[] };
  states?: { working?: StateChange; waiting?: StateChange; stopped?: StateChange };
  roleDefaults?: Record<string, ComposedAvatar>;
  definition: object;
}

/** How a member's state shows on its avatar. */
export type AvatarState = 'idle' | 'working' | 'waiting' | 'stopped';

export function avatarStateOf(status: Bot['status'] | undefined): AvatarState {
  switch (status) {
    case 'working':
      return 'working';
    case 'waiting_approval':
    case 'waiting_input':
      return 'waiting';
    case 'interrupted':
      return 'stopped';
    default:
      return 'idle';
  }
}

export const FALLBACK_AVATAR = fallbackUrl;

function composedData(series: AvatarSeries): ComposedSeriesData | null {
  if (series.kind !== 'composed') return null;
  const data = series.data as unknown as ComposedSeriesData;
  if (data?.schemaVersion !== 1 || !data.parts || !data.definition) return null;
  return data;
}

/** The first composed series of the catalog: the one built-in avatars use. */
export function defaultSeries(catalog: AvatarCatalog | undefined): AvatarSeries | null {
  return catalog?.series.find((series) => composedData(series) !== null) ?? null;
}

// FNV-1a: a stable pick for roles the catalog has no default for.
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** The avatar a role gets from the series: its default, or a stable pick. */
export function roleAvatar(series: AvatarSeries, roleId: string): ComposedAvatar | null {
  const data = composedData(series);
  if (!data) return null;
  const preset = data.roleDefaults?.[roleId];
  if (preset) return preset;
  const { shapes, eyes, palette } = data.parts;
  if (!shapes.length || !eyes.length || !palette.length) return null;
  const h = hash(roleId || 'member');
  return { shape: shapes[h % shapes.length], eyes: eyes[(h >>> 8) % eyes.length], color: palette[(h >>> 16) % palette.length] };
}

const styles = new Map<string, Style>();
const drawn = new Map<string, string>();

/** A data URI for one avatar of a series in one state; cached. */
export function drawAvatar(series: AvatarSeries, avatar: ComposedAvatar, state: AvatarState = 'idle'): string {
  const data = composedData(series);
  if (!data) return FALLBACK_AVATAR;
  const key = `${series.id}@${series.version}|${avatar.shape}|${avatar.eyes}|${avatar.color}|${state}`;
  const cached = drawn.get(key);
  if (cached) return cached;
  try {
    const styleKey = `${series.id}@${series.version}`;
    let style = styles.get(styleKey);
    if (!style) {
      style = new Style(data.definition as ConstructorParameters<typeof Style>[0]);
      styles.set(styleKey, style);
    }
    const change = state === 'idle' ? {} : (data.states?.[state] ?? {});
    const uri = new Avatar(style, {
      seed: `${avatar.shape}-${avatar.eyes}-${avatar.color}`,
      ...data.render,
      shapeVariant: avatar.shape,
      eyesVariant: change.eyes ?? avatar.eyes,
      bodyColor: avatar.color,
      animationVariant: change.animation ?? 'none',
    }).toDataUri();
    drawn.set(key, uri);
    return uri;
  } catch {
    // A part this build cannot draw (a newer catalog, a removed part).
    return FALLBACK_AVATAR;
  }
}
