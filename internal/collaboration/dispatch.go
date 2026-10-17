package collaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

// App is the slice of application services the collaboration tools need. It
// is an interface so the dispatcher stays testable and never reaches into the
// database itself.
type App interface {
	ValidateToolRun(ctx context.Context, principal auth.Principal) error
	ViewTasks(ctx context.Context, principal auth.Principal, taskID string) (map[string]any, error)
	DeliverTask(ctx context.Context, principal auth.Principal, taskID, summary string, artifactRefs []string) (domain.Task, error)
	ReviewTask(ctx context.Context, principal auth.Principal, taskID string, accept bool, comment string) (domain.Task, error)
	WaitingTasksOf(ctx context.Context, teamID, botID string) []string
	SendMessageFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string) (map[string]any, error)
	NotifyFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string) (map[string]any, error)
	CreateTask(ctx context.Context, principal auth.Principal, captainOnly bool, in app.CreateTaskInput) (domain.Task, error)
	ListTasks(ctx context.Context, principal auth.Principal, teamID, ownerID string, limit int) ([]domain.Task, error)
	GetTaskDetail(ctx context.Context, principal auth.Principal, taskID string) (map[string]any, error)
	AssignTask(ctx context.Context, principal auth.Principal, taskID, newOwnerID string, expectedRevision int64) (domain.Task, error)
	UpdateTaskStatus(ctx context.Context, principal auth.Principal, taskID, status, blockedReason string, expectedRevision int64) (domain.Task, error)
	ReportCompletion(ctx context.Context, principal auth.Principal, taskID string, evidenceIDs []string, expectedRevision int64) (domain.Task, error)
	SubmitEvidence(ctx context.Context, principal auth.Principal, taskID, summary string, artifactRefs []string, candidateRef, testResult, reviewResult string) (domain.Evidence, error)
	ConfirmTask(ctx context.Context, principal auth.Principal, taskID string, evidenceIDs []string, expectedRevision int64) (domain.Task, error)
	OfferTransfer(ctx context.Context, principal auth.Principal, taskID, toBotID, note string) (domain.Transfer, error)
	AcceptTransfer(ctx context.Context, principal auth.Principal, transferID string) (domain.Task, error)
	AskUser(ctx context.Context, principal auth.Principal, in app.AskUserInput) (domain.Question, error)
	RequestMember(ctx context.Context, principal auth.Principal, roleID, reason string) (domain.Approval, error)
	RoleLibrary(ctx context.Context) ([]map[string]any, error)
	BuildBotContext(ctx context.Context, principal auth.Principal, taskID string) (string, error)
	BotOf(ctx context.Context, botID string) (domain.Bot, error)
	CaptainOf(ctx context.Context, teamID string) (string, error)
	SubmitMergeRequest(ctx context.Context, principal auth.Principal, taskID, candidateRef, targetRef, executionDirectory string, evidenceIDs []string) (domain.Approval, error)
	RememberMemory(ctx context.Context, principal auth.Principal, body, sourceTaskID string) (domain.Memory, error)
	SearchMemories(ctx context.Context, principal auth.Principal, query string, limit int) (map[string]any, error)
	// Write idempotency (E05): a retried side-effect call replays its stored
	// result; the same key with different arguments is a conflict.
	WriteReplay(ctx context.Context, scope, principal, key, argsHash string) (string, bool, error)
}

// Dispatcher validates a bot's identity and routes one tool call onto the
// application layer. Both the user API and the MCP API end up in the same app
// methods, so the permission rules live in exactly one place.
type Dispatcher struct {
	app    App
	tokens *auth.TokenStore
}

// NewDispatcher wires the dispatcher to the application services and the
// in-memory token store.
func NewDispatcher(application App, tokens *auth.TokenStore) *Dispatcher {
	return &Dispatcher{app: application, tokens: tokens}
}

// ResolveToken maps a bridge credential onto a bot principal.
func (d *Dispatcher) ResolveToken(token string) (auth.Principal, auth.BotBinding, error) {
	binding, err := d.tokens.Resolve(token)
	if err != nil {
		return auth.Principal{}, auth.BotBinding{}, err
	}
	principal := auth.NewPrincipalBot(binding.BotID, binding.TeamID)
	principal.RunID = binding.RunID
	return principal, binding, nil
}

// dispatchTimeout bounds every bridge-driven call so a stuck application call
// can never hang the engine's tool loop.
const dispatchTimeout = 30 * time.Second

// ToolCall is one MCP tool invocation arriving from the bridge.
type ToolCall struct {
	Name      string
	Arguments json.RawMessage
	// CallID is the engine's id for this call (bridge transport metadata).
	CallID string
}

// withDerivedKey gives a write without an idempotencyKey one derived from the
// engine's call id and the arguments: a retry of the same call replays, two
// different calls inside one code cell (which share the call id) stay apart,
// and the model never has to invent a key.
func withDerivedKey(call ToolCall) ToolCall {
	if call.CallID == "" || !IsSideEffectTool(call.Name) {
		return call
	}
	args := map[string]any{}
	if len(call.Arguments) > 0 && json.Unmarshal(call.Arguments, &args) != nil {
		return call
	}
	if key, _ := args["idempotencyKey"].(string); key != "" {
		return call
	}
	hash := canonicalArgsHash(call.Arguments)
	args["idempotencyKey"] = "call:" + call.CallID + ":" + hash[:16]
	if len(args["idempotencyKey"].(string)) > 128 {
		args["idempotencyKey"] = "call:" + hash
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return call
	}
	call.Arguments = raw
	return call
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// toolError maps a product error onto the wire contract's rejected status.
func toolError(err error) *ToolError {
	if err == nil {
		return nil
	}
	var te *ToolError
	if ok := asToolError(err, &te); ok {
		return te
	}
	de := domain.AsError(err)
	status := StatusRejected
	switch de.Code {
	case domain.CodeEngineUnavailable, domain.CodeCapabilityMissing:
		status = StatusUnavailable
	case domain.CodeConflict, domain.CodeRevisionConflict, domain.CodeIdempotencyConflict:
		status = StatusRejected
	}
	return &ToolError{Status: status, Code: string(de.Code), MessageKey: de.MessageKey, Params: de.Params}
}

func asToolError(err error, target **ToolError) bool {
	te, ok := err.(*ToolError)
	if ok {
		*target = te
	}
	return ok
}

// CallTool routes one tool by name with write idempotency (E05): a retried
// side-effect call carrying the same key and the same arguments replays the
// stored result instead of creating a second resource, and the same key with
// different arguments is rejected. Read tools and keyless calls pass through
// to the router, where each write tool still enforces key presence.
func (d *Dispatcher) CallTool(ctx context.Context, principal auth.Principal, call ToolCall) ToolResult {
	call = withDerivedKey(call)
	if IsSideEffectTool(call.Name) {
		if err := d.app.ValidateToolRun(ctx, principal); err != nil {
			return errorResult(err)
		}
	}
	switch call.Name {
	case "assign_task", "update_task", "report_completion", "confirm_task", "accept_transfer":
		var version struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if json.Unmarshal(call.Arguments, &version) != nil || version.ExpectedRevision < 1 {
			return rejected("invalid_request", "error.revision.required", nil)
		}
	}
	var replayKey, replayHash string
	if IsSideEffectTool(call.Name) && principal.Kind == auth.KindBot && principal.BotID != "" {
		var probe struct {
			RequestID string `json:"idempotencyKey"`
		}
		if err := json.Unmarshal(call.Arguments, &probe); err == nil && probe.RequestID != "" {
			replayKey = probe.RequestID
			replayHash = canonicalArgsHash(call.Arguments)
			cached, hit, rerr := d.app.WriteReplay(ctx, "mcp."+call.Name, principal.BotID, replayKey, replayHash)
			if rerr != nil {
				return errorResult(rerr)
			}
			if hit {
				var stored ToolResult
				if json.Unmarshal([]byte(cached), &stored) == nil {
					return stored
				}
			}
			principal.Write = &auth.WriteRequest{Scope: "mcp." + call.Name, Key: replayKey, ArgsHash: replayHash}
		}
	}
	return d.callToolOnce(ctx, principal, call)
}

// canonicalArgsHash fingerprints arguments with stable key order, so a retry
// that reserializes the same object still matches the stored request.
func canonicalArgsHash(raw json.RawMessage) string {
	var canon any
	if err := json.Unmarshal(raw, &canon); err != nil {
		canon = string(raw)
	}
	stable, _ := json.Marshal(canon)
	sum := sha256.Sum256(stable)
	return hex.EncodeToString(sum[:])
}

// callToolOnce is the routing switch itself (pre-idempotency behavior).
func (d *Dispatcher) callToolOnce(ctx context.Context, principal auth.Principal, call ToolCall) ToolResult {
	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	switch call.Name {
	case "view_tasks":
		var args struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		data, err := d.app.ViewTasks(ctx, principal, args.TaskID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusOK, Data: data}

	case "deliver_task":
		var args struct {
			TaskID       string   `json:"taskId"`
			Summary      string   `json:"summary"`
			ArtifactRefs []string `json:"artifactRefs"`
			RequestID    string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		task, err := d.app.DeliverTask(ctx, principal, args.TaskID, args.Summary, args.ArtifactRefs)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "review_task":
		var args struct {
			TaskID    string `json:"taskId"`
			Decision  string `json:"decision"`
			Comment   string `json:"comment"`
			RequestID string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		if args.Decision != "accept" && args.Decision != "reject" {
			return rejected("invalid_request", "error.task.review_decision", nil)
		}
		task, err := d.app.ReviewTask(ctx, principal, args.TaskID, args.Decision == "accept", args.Comment)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "get_context":
		var args struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		text, err := d.app.BuildBotContext(ctx, principal, args.TaskID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusOK, Data: map[string]any{"context": text}}

	case "list_tasks":
		var args struct {
			Status string `json:"status"`
			Limit  int    `json:"limit"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		bot, err := d.requireBot(ctx, principal)
		if err != nil {
			return errorResult(err)
		}
		tasks, err := d.app.ListTasks(ctx, principal, bot.TeamID, "", args.Limit)
		if err != nil {
			return errorResult(err)
		}
		if args.Status != "" {
			filtered := tasks[:0]
			for _, t := range tasks {
				if t.Status == args.Status {
					filtered = append(filtered, t)
				}
			}
			tasks = filtered
		}
		return ToolResult{Status: StatusOK, Data: map[string]any{"tasks": tasks}}

	case "get_task":
		var args struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil || args.TaskID == "" {
			return rejected("invalid_request", "error.task.id_required", nil)
		}
		detail, err := d.app.GetTaskDetail(ctx, principal, args.TaskID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusOK, Data: detail}

	case "list_roles":
		items, err := d.app.RoleLibrary(ctx)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusOK, Data: map[string]any{"roles": items}}

	case "send_message":
		var args struct {
			TargetBotID  string   `json:"targetBotId"`
			TargetBotIDs []string `json:"targetBotIds"`
			Text         string   `json:"text"`
			TaskID       string   `json:"taskId"`
			NotifyOnly   bool     `json:"notifyOnly"`
			RequestID    string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		targets := uniqueTargets(args.TargetBotID, args.TargetBotIDs)
		if len(targets) == 0 {
			return rejected("invalid_request", "error.message.target_required", nil)
		}
		send := d.app.SendMessageFromBot
		if args.NotifyOnly {
			send = d.app.NotifyFromBot
		}
		if len(targets) == 1 {
			result, err := send(ctx, principal, targets[0], args.Text, args.TaskID, args.RequestID)
			if err != nil {
				return errorResult(err)
			}
			d.noteWaiting(ctx, principal, targets[0], result)
			return ToolResult{Status: StatusAccepted, Data: result}
		}
		// One message per recipient, each its own idempotent write so a
		// retried broadcast neither duplicates nor drops a recipient.
		results := make([]map[string]any, 0, len(targets))
		for _, target := range targets {
			result, err := send(ctx, perRecipient(principal, target), target, args.Text, args.TaskID, recipientKey(args.RequestID, target))
			if err != nil {
				return errorResult(err)
			}
			result["targetBotId"] = target
			d.noteWaiting(ctx, principal, target, result)
			results = append(results, result)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"status": "accepted", "recipients": results}}

	case "create_task":
		var args struct {
			Title              string   `json:"title"`
			Description        string   `json:"description"`
			AcceptanceCriteria string   `json:"acceptanceCriteria"`
			OwnerID            string   `json:"ownerId"`
			ParentID           string   `json:"parentId"`
			DependencyIDs      []string `json:"dependencyIds"`
			RequestID          string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		bot, err := d.requireBot(ctx, principal)
		if err != nil {
			return errorResult(err)
		}
		task, err := d.app.CreateTask(ctx, principal, true, app.CreateTaskInput{
			TeamID: bot.TeamID, OwnerID: args.OwnerID, Title: args.Title,
			Description: args.Description, AcceptanceCriteria: args.AcceptanceCriteria,
			ParentID: args.ParentID, DependencyIDs: args.DependencyIDs, RequestID: args.RequestID,
		})
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "propose_task":
		// A member cannot create a task; it forms a proposal message the
		// captain acts on, so the permission rule stays real.
		var args struct {
			Title              string `json:"title"`
			Description        string `json:"description"`
			AcceptanceCriteria string `json:"acceptanceCriteria"`
			RequestID          string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		bot, err := d.requireBot(ctx, principal)
		if err != nil {
			return errorResult(err)
		}
		team, err := d.captainOf(ctx, bot.TeamID)
		if err != nil {
			return errorResult(err)
		}
		proposal := "任务提议\n标题：" + args.Title + "\n描述：" + args.Description + "\n完成标准：" + args.AcceptanceCriteria
		result, err := d.app.SendMessageFromBot(ctx, principal, team, proposal, "", args.RequestID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: result}

	case "assign_task":
		var args struct {
			TaskID           string `json:"taskId"`
			OwnerID          string `json:"ownerId"`
			ExpectedRevision int64  `json:"expectedRevision"`
			RequestID        string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		task, err := d.app.AssignTask(ctx, principal, args.TaskID, args.OwnerID, args.ExpectedRevision)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "update_task":
		var args struct {
			TaskID           string `json:"taskId"`
			Status           string `json:"status"`
			BlockedReason    string `json:"blockedReason"`
			ExpectedRevision int64  `json:"expectedRevision"`
			RequestID        string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		task, err := d.app.UpdateTaskStatus(ctx, principal, args.TaskID, args.Status, args.BlockedReason, args.ExpectedRevision)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "submit_evidence":
		var args struct {
			TaskID       string   `json:"taskId"`
			Summary      string   `json:"summary"`
			ArtifactRefs []string `json:"artifactRefs"`
			CandidateRef string   `json:"candidateRef"`
			TestResult   string   `json:"testResult"`
			ReviewResult string   `json:"reviewResult"`
			RequestID    string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		evidence, err := d.app.SubmitEvidence(ctx, principal, args.TaskID, args.Summary, args.ArtifactRefs, args.CandidateRef, args.TestResult, args.ReviewResult)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"evidence": evidence}}

	case "report_completion":
		// Reporting completion moves the task to review; only the confirmer
		// can mark it done, so a bot can never self-certify.
		var args struct {
			TaskID           string   `json:"taskId"`
			EvidenceIDs      []string `json:"evidenceIds"`
			ExpectedRevision int64    `json:"expectedRevision"`
			RequestID        string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		task, err := d.app.ReportCompletion(ctx, principal, args.TaskID, args.EvidenceIDs, args.ExpectedRevision)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "confirm_task":
		var args struct {
			TaskID           string   `json:"taskId"`
			EvidenceIDs      []string `json:"evidenceIds"`
			ExpectedRevision int64    `json:"expectedRevision"`
			RequestID        string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		task, err := d.app.ConfirmTask(ctx, principal, args.TaskID, args.EvidenceIDs, args.ExpectedRevision)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "offer_transfer":
		var args struct {
			TaskID    string `json:"taskId"`
			ToBotID   string `json:"toBotId"`
			Note      string `json:"note"`
			RequestID string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		transfer, err := d.app.OfferTransfer(ctx, principal, args.TaskID, args.ToBotID, args.Note)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"transfer": transfer}}

	case "accept_transfer":
		var args struct {
			TransferID       string `json:"transferId"`
			ExpectedRevision int64  `json:"expectedRevision"`
			RequestID        string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		principal.Write.ExpectedRevision = args.ExpectedRevision
		task, err := d.app.AcceptTransfer(ctx, principal, args.TransferID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"task": task}}

	case "ask_user":
		var args struct {
			Question  string   `json:"question"`
			Options   []string `json:"options"`
			TaskID    string   `json:"taskId"`
			RequestID string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		question, err := d.app.AskUser(ctx, principal, app.AskUserInput{
			Question: args.Question, Options: args.Options, TaskID: args.TaskID,
		})
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusWaiting, Data: map[string]any{"questionId": question.ID}}

	case "request_member":
		var args struct {
			RoleID    string `json:"roleId"`
			Reason    string `json:"reason"`
			RequestID string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		approval, err := d.app.RequestMember(ctx, principal, args.RoleID, args.Reason)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusWaiting, Data: map[string]any{"approvalId": approval.ID}}

	case "request_merge":
		var args struct {
			TaskID             string   `json:"taskId"`
			CandidateRef       string   `json:"candidateRef"`
			TargetRef          string   `json:"targetRef"`
			ExecutionDirectory string   `json:"executionDirectory"`
			EvidenceIDs        []string `json:"evidenceIds"`
			RequestID          string   `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		approval, err := d.app.SubmitMergeRequest(ctx, principal, args.TaskID, args.CandidateRef, args.TargetRef, args.ExecutionDirectory, args.EvidenceIDs)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusWaiting, Data: map[string]any{"approvalId": approval.ID}}

	case "remember":
		// Both spellings from the plan table (content/taskId) and the
		// implementation (body/sourceTaskId) are accepted, so the tool is
		// usable whichever name the caller follows.
		var args struct {
			Body         string `json:"body"`
			Content      string `json:"content"`
			SourceTaskID string `json:"sourceTaskId"`
			TaskID       string `json:"taskId"`
			RequestID    string `json:"idempotencyKey"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		if args.RequestID == "" {
			return rejected("invalid_request", "error.idempotency.required", nil)
		}
		body := args.Body
		if body == "" {
			body = args.Content
		}
		sourceTask := args.SourceTaskID
		if sourceTask == "" {
			sourceTask = args.TaskID
		}
		mem, err := d.app.RememberMemory(ctx, principal, body, sourceTask)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: map[string]any{"memory": mem}}

	case "search_memory":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return rejected("invalid_request", "error.invalid_json", nil)
		}
		result, err := d.app.SearchMemories(ctx, principal, args.Query, args.Limit)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusOK, Data: result}

	default:
		return rejected("invalid_request", "error.tool.unknown", map[string]any{"tool": call.Name})
	}
}

func rejected(code, messageKey string, params map[string]any) ToolResult {
	return ToolResult{Status: StatusRejected, Error: withHint(&ToolError{Status: StatusRejected, Code: code, MessageKey: messageKey, Params: params})}
}

func errorResult(err error) ToolResult {
	te := withHint(toolError(err))
	return ToolResult{Status: te.Status, Error: te}
}

// requireBot resolves the caller's bot row, proving the token still maps to a
// live member.
func (d *Dispatcher) requireBot(ctx context.Context, principal auth.Principal) (domain.Bot, error) {
	if principal.Kind != auth.KindBot {
		return domain.Bot{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	return d.app.BotOf(ctx, principal.BotID)
}

func (d *Dispatcher) captainOf(ctx context.Context, teamID string) (string, error) {
	return d.app.CaptainOf(ctx, teamID)
}

// uniqueTargets is the send_message recipient list: targetBotIds, or the
// single legacy targetBotId, without repeats.
func uniqueTargets(single string, many []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append([]string{single}, many...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// recipientKey derives one recipient's idempotency key from the broadcast's.
func recipientKey(key, target string) string {
	if len(key) > 100 {
		key = key[:100]
	}
	suffix := target
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	return key + "/" + suffix
}

// perRecipient gives each recipient's write its own idempotency identity.
func perRecipient(p auth.Principal, target string) auth.Principal {
	if p.Write == nil {
		return p
	}
	write := *p.Write
	write.Key = recipientKey(write.Key, target)
	p.Write = &write
	return p
}

// noteWaiting tells the sender when the recipient's task still waits for
// another one, so nobody is set to work before its task has reached it.
func (d *Dispatcher) noteWaiting(ctx context.Context, principal auth.Principal, target string, result map[string]any) {
	if waiting := d.app.WaitingTasksOf(ctx, principal.TeamID, target); len(waiting) > 0 {
		result["note"] = "对方的任务还没送到它手上：" + strings.Join(waiting, "；") + "。前置任务完成后系统会自动把任务送过去，不必现在叫它开工。"
	}
}
