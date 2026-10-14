// Aliases onto the generated wire types so features import from one place.
import type { components } from './generated/schema';

export type Status = components['schemas']['Status'];
export type EngineStatus = components['schemas']['EngineStatus'];
export type Settings = components['schemas']['Settings'];
export type SettingsPatch = components['schemas']['SettingsPatch'];
export type Snapshot = components['schemas']['Snapshot'];
export type Event = components['schemas']['Event'];
export type ErrorEnvelope = components['schemas']['ErrorEnvelope'];
