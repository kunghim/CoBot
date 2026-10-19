// Aliases onto the generated wire types so features import from one place.
// Entity shapes come from OpenAPI; page composites add local view metadata.
import type { components } from './generated/schema';

export type Status = components['schemas']['Status'];
export type Settings = components['schemas']['Settings'];
export type AvatarCatalog = components['schemas']['AvatarCatalog'];
export type AvatarSeries = components['schemas']['AvatarSeries'];
export type AvatarRef = components['schemas']['AvatarRef'];
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

export interface EngineModel {
  slug?: string;
  id?: string;
  displayName?: string;
  description?: string;
  family?: string;
  isDefault?: boolean;
  /** The effort the model uses when none is chosen. */
  defaultReasoningEffort?: string;
  /** The reasoning efforts the model offers. */
  reasoningEfforts?: Array<{ effort: string; description?: string }>;
}

/** GET /bots/{id}/stats: a member's conversation in numbers. */
export type SessionStats = components['schemas']['SessionStats'];

export interface ModelListResult {
  models: EngineModel[];
}

// ---------------------------------------------------------------------------
// Teams and members
// ---------------------------------------------------------------------------

export type TeamStatus = 'active' | 'archived' | 'deleted' | 'directory_missing';

export type Team = components['schemas']['Team'];

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
  /** The avatar the user chose for the role; absent follows the catalog. */
  avatar?: AvatarRef;
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

export type Run = components['schemas']['Run'];

/** How freely a team's members may act; applies from each next turn. */
export type PermissionMode = 'read_only' | 'default' | 'full';

/** One visible step of a run (thinking, a command, a file change, a tool
 * call, a reconnect, an engine notice). `detail` is kind-specific. */
export type RunStepKind = 'reasoning' | 'message' | 'agent' | 'diff' | 'command' | 'file_change' | 'tool' | 'web_search' | 'plan' | 'retry' | 'notice';
export type RunStepStatus = 'in_progress' | 'completed' | 'failed' | 'declined' | 'interrupted';
export interface RunStep {
  id: string;
  runId: string;
  teamId: string;
  botId: string;
  seq: number;
  kind: RunStepKind | string;
  status: RunStepStatus | string;
  detail: Record<string, unknown>;
  startedAt: string;
  updatedAt: string;
}

export type RunWithSteps = Run & { steps: RunStep[] };

/** GET /bots/{id}/messages */
export interface ConversationResult {
  messages: Message[];
  queuedCount: number;
  bot: Bot;
  activeRun: Run | null;
  /** Runs started between this page's messages, with their steps. */
  runs?: RunWithSteps[];
  nextCursor: string;
}

/** A transient `cobot.live` stream frame: a step that changed or streamed
 * text of a running turn. */
export interface LiveSignal {
  type: 'step' | 'delta';
  teamId: string;
  botId: string;
  runId: string;
  step?: RunStep;
  itemId?: string;
  kind?: 'message' | 'commentary' | 'reasoning' | 'output';
  text?: string;
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

/** GET /teams/{id}/activity — one durable product event plus the facts to
 * describe it (task title, who sent what to whom, ...); absent for records
 * deleted since. */
export type ActivityItem = components['schemas']['Event'] & { detail?: Record<string, unknown> };

/** GET /bots/{id}/memories */
export interface MemoryListResult {
  items: Memory[];
  nextCursor: string;
}

export type Memory = components['schemas']['Memory'];

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
// Snapshot: settings, teams and engine at one event seq, for startup
// ---------------------------------------------------------------------------

export type Snapshot = components['schemas']['Snapshot'];

// ---------------------------------------------------------------------------
// Requirements, cards, handoff records, questions
// ---------------------------------------------------------------------------

/** A card turns doing when its holder's turn takes it up and done when the
 * holder hands it off; a cancelled requirement cancels its open cards. */
export type TaskStatus = 'todo' | 'doing' | 'done' | 'cancelled';

export type Task = components['schemas']['Task'];

export type Evidence = components['schemas']['Evidence'];

/** What the engine reported a member ran and changed while it held a card. */
export type Execution = components['schemas']['Execution'];

export type Stage = components['schemas']['Stage'];

/** The project manager's requirement document and the route the user confirmed. */
export type Requirement = components['schemas']['Requirement'];

/** GET /teams/{id}/requirements */
export interface RequirementListResult {
  items: Requirement[];
}

/** GET /teams/{id}/tasks */
export interface TaskListResult {
  items: Task[];
  nextCursor: string;
}

/** GET /tasks/{id} */
export interface TaskDetailResult {
  task: Task;
  evidence: Evidence[];
}

/** ask_user output: user input, never a security authorization. */
export type Question = components['schemas']['Question'];

/** GET /questions?teamId&status */
export interface QuestionListResult {
  items: Question[];
  nextCursor: string;
}

