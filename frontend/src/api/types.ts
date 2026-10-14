// Aliases onto the generated wire types so features import from one place.
// The M2 surface (teams, conversations, approvals, engine, models) is typed
// with hand-written interfaces that mirror the Go wire structs; schema.ts is
// regenerated from api/openapi.yaml by the main agent and must not be edited
// here.
import type { components } from './generated/schema';

export type Status = components['schemas']['Status'];
export type Settings = components['schemas']['Settings'];
export type SettingsPatch = components['schemas']['SettingsPatch'];
export type Event = components['schemas']['Event'];
export type ErrorEnvelope = components['schemas']['ErrorEnvelope'];

// ---------------------------------------------------------------------------
// Engine (Codex) projections
// ---------------------------------------------------------------------------

/** GET /engine and the snapshot's cached engine projection. */
export interface EngineStatus {
  status: 'not_configured' | 'ready' | 'error' | 'starting';
  binaryPath?: string;
  version?: string;
  protocol?: string;
  loggedIn: boolean;
  /** Only present on the snapshot projection; GET /engine folds it into
   * capabilities as `authMode:<mode>`. */
  authMode?: string;
  capabilities?: string[];
  errorKey?: string;
  /** Snapshot projection only: when the status was last probed. */
  checkedAt?: string;
}

/** GET /models: engine-provided, sanitized model list. */
export interface EngineModel {
  slug?: string;
  id?: string;
  displayName?: string;
  description?: string;
  family?: string;
}

export interface ModelListResult {
  models: EngineModel[];
}

// ---------------------------------------------------------------------------
// Teams and members
// ---------------------------------------------------------------------------

export type TeamStatus = 'creating' | 'active' | 'archived' | 'deleted' | 'directory_missing' | 'error';

export interface Team {
  id: string;
  name: string;
  profileInstanceId: string;
  leaderId: string;
  directory: string;
  status: TeamStatus;
  statusDetail?: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
}

export type BotStatus = 'idle' | 'queued' | 'working' | 'waiting_approval' | 'waiting_input' | 'interrupted' | 'unknown';

export interface Bot {
  id: string;
  teamId: string;
  name: string;
  roleId: string;
  roleVersion?: string;
  isLeader: boolean;
  status: BotStatus;
  currentRunId?: string;
  revision: number;
  createdAt: string;
}

/** One team with its members; the list projection used everywhere. */
export interface TeamWithBots {
  team: Team;
  bots: Bot[];
}

export interface TeamListResult {
  items: TeamWithBots[];
  nextCursor: string;
}

export interface CreateTeamResult {
  team: Team;
  bots: Bot[];
}

/** GET /roles: the editable-copy role library. */
export interface Role {
  id: string;
  nameKey: string;
  name: string;
  contentVersion?: string;
}

export interface RoleListResult {
  items: Role[];
  nextCursor: string;
}

// ---------------------------------------------------------------------------
// Conversations, messages and runs
// ---------------------------------------------------------------------------

export type MessageKind = 'user' | 'assistant' | 'collaboration' | 'system';

/** accepted only means the engine accepted the turn, never that it is done. */
export type DeliveryStatus =
  | 'waiting_dependency'
  | 'queued'
  | 'dispatching'
  | 'accepted'
  | 'cancelled'
  | 'unknown';

export interface TextBlock {
  type: string;
  text: string;
}

export interface Message {
  id: string;
  teamId: string;
  botId: string;
  kind: MessageKind;
  /** Wire value is a JSON array of blocks; messageText() tolerates a string
   * form defensively. */
  blocks: TextBlock[];
  requestId?: string;
  deliveryStatus?: DeliveryStatus;
  runId?: string;
  createdAt: string;
}

export type RunStatus = 'starting' | 'running' | 'waiting_approval' | 'waiting_input' | 'completed' | 'failed' | 'interrupted' | 'unknown';

export interface Run {
  id: string;
  teamId: string;
  botId: string;
  providerSessionId?: string;
  providerTurnId?: string;
  engineEpoch?: string;
  status: RunStatus;
  errorKey?: string;
  startedAt: string;
  endedAt?: string;
}

/** GET /bots/{id}/messages */
export interface ConversationResult {
  messages: Message[];
  queuedCount: number;
  bot: Bot;
  activeRun: Run | null;
}

/** POST /bots/{id}/messages (202: queued, not delivered). */
export interface SendMessageResult {
  message: Message;
  deliveryStatus: DeliveryStatus;
  queuedAhead: number;
}

/** POST /runs/{id}/interrupt (202: request accepted, never a claim of stop). */
export interface InterruptRunResult {
  runId: string;
  interruptRequested: boolean;
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

export type ApprovalRequestState = 'pending' | 'decided' | 'invalidated';

/** queued/sent/confirmed/unknown/expired: a decision is not a confirmed reply. */
export type ApprovalResponseState = 'queued' | 'sent' | 'confirmed' | 'unknown' | 'expired';

export type ApprovalType = 'native_tool' | 'collaboration' | 'recruitment' | 'recovery' | 'merge';

export interface ApprovalOption {
  id: string;
  messageKey?: string;
  text?: string;
}

export interface ApprovalDecision {
  optionId: string;
  comment?: string;
  actor: string;
  decidedAt: string;
}

export interface Approval {
  id: string;
  teamId: string;
  requesterBotId: string;
  runId?: string;
  type: ApprovalType;
  method?: string;
  summary: string;
  reason?: string;
  options: ApprovalOption[];
  requestState: ApprovalRequestState;
  decision?: ApprovalDecision;
  responseState?: ApprovalResponseState;
  createdAt: string;
  revision: number;
}

export interface ApprovalListResult {
  items: Approval[];
  nextCursor: string;
}

// ---------------------------------------------------------------------------
// Snapshot (hand-written: the generated one predates teams/engine)
// ---------------------------------------------------------------------------

export interface Snapshot {
  seq: number;
  instanceId: string;
  settings: Settings;
  teams: TeamWithBots[];
  engine: EngineStatus;
}
