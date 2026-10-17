// Aliases onto the generated wire types so features import from one place.
// Entity shapes come from OpenAPI; page composites add local view metadata.
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

export type EngineLogin = components['schemas']['EngineLogin'];

export interface EngineModel { slug?: string; id?: string; displayName?: string; description?: string; family?: string }

export interface ModelListResult {
  models: EngineModel[];
}

// ---------------------------------------------------------------------------
// Teams and members
// ---------------------------------------------------------------------------

export type TeamStatus = 'creating' | 'active' | 'archived' | 'deleted' | 'directory_missing' | 'error';

export type Team = components['schemas']['Team'];

export type BotStatus = 'idle' | 'queued' | 'working' | 'waiting_approval' | 'waiting_input' | 'interrupted' | 'unknown';

export type Bot = components['schemas']['Bot'];

/** One team with its members; the list projection used everywhere. */
export type TeamWithBots = components['schemas']['TeamWithBots'];

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
  summaryKey?: string;
  name: string;
  contentVersion?: string;
  scenarios?: string[];
}

/** A built-in scenario groups roles and preselects its usual members. */
export interface RoleScenario {
  id: string;
  nameKey: string;
  name: string;
  roleIds: string[];
  defaultRoleIds: string[];
}

export interface RoleListResult {
  items: Role[];
  scenarios?: RoleScenario[];
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

export type TextBlock = components['schemas']['TextBlock'];

export type Message = components['schemas']['Message'];

export type RunStatus = 'starting' | 'running' | 'waiting_approval' | 'waiting_input' | 'completed' | 'failed' | 'interrupted' | 'unknown';

export type Run = components['schemas']['Run'];

/** GET /bots/{id}/messages */
export interface ConversationResult {
  messages: Message[];
  queuedCount: number;
  bot: Bot;
  activeRun: Run | null;
  nextCursor: string;
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
  responseState: 'sent' | 'unknown';
  messageKey?: string;
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

export type ApprovalRequestState = 'pending' | 'decided' | 'invalidated';

/** queued/sent/confirmed/unknown/expired: a decision is not a confirmed reply. */
export type ApprovalResponseState = 'queued' | 'sent' | 'confirmed' | 'unknown' | 'expired';

export type ApprovalType = 'native_tool' | 'collaboration' | 'recruitment' | 'recovery' | 'merge' | 'chain';

export type ApprovalOption = components['schemas']['ApprovalOption'];

export type ApprovalDecision = components['schemas']['ApprovalDecision'];

export type Approval = components['schemas']['Approval'];

export interface ApprovalListResult {
  total?: number;
  items: Approval[];
  nextCursor: string;
}


// ---------------------------------------------------------------------------
// M5: activity, memories, directories, authorizations, team management
// ---------------------------------------------------------------------------

/** GET /teams/{id}/activity — one durable product event. */
export type ActivityItem = components['schemas']['Event'];

/** GET /bots/{id}/memories */
export interface MemoryListResult {
  items: Memory[];
  nextCursor: string;
}

export type Memory = components['schemas']['Memory'];

/** GET /directories — subdirectory names only. */
export interface DirectoryListResult {
  path: string;
  children: string[];
}

/** POST /directories/check */
export interface DirectoryCheckResult {
  canonical: string;
  ok: boolean;
  duplicate: boolean;
  nested: boolean;
  /** Another team, possibly in another CoBot instance, claims the folder. */
  claimed: boolean;
  /** The folder belongs to an archived team; restore it to use the folder. */
  archived?: boolean;
  teamId?: string;
  teamName?: string;
}

/** GET /authorizations — live bridge credentials, never token values. */
export interface AuthorizationBinding {
  botId: string;
  teamId: string;
  engineEpoch: string;
  expiresAt: string;
}

// ---------------------------------------------------------------------------
// Snapshot (hand-written: the generated one predates teams/engine)
// ---------------------------------------------------------------------------

export type Snapshot = components['schemas']['Snapshot'];

// ---------------------------------------------------------------------------
// M3: tasks, evidence, transfers, questions, collaboration records
// ---------------------------------------------------------------------------

/** done only through confirm (evidence-backed); a chat message never changes
 * status by itself. */
export type TaskStatus = 'todo' | 'doing' | 'review' | 'done' | 'cancelled';

export type Task = components['schemas']['Task'];

export type Evidence = components['schemas']['Evidence'];

export type TransferStatus = 'offered' | 'accepted' | 'cancelled' | 'stale';

export type Transfer = components['schemas']['Transfer'];

/** GET /teams/{id}/tasks */
export interface TaskListResult {
  items: Task[];
  nextCursor: string;
}

/** GET /tasks/{id} */
export interface TaskDetailResult {
  task: Task;
  evidence: Evidence[];
  transfers: Transfer[];
}

export type QuestionStatus = 'open' | 'answered' | 'cancelled';

/** ask_user output: user input, never a security authorization. */
export type Question = components['schemas']['Question'];

/** GET /questions?teamId&status */
export interface QuestionListResult {
  items: Question[];
  nextCursor: string;
}

/** GET /bots/{id}/collaboration */
export interface CollaborationListResult {
  items: Message[];
  nextCursor: string;
}
