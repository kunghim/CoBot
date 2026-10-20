package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/kunghim/CoBot/internal/failure"
)

// Stable machine codes for a connectivity check. They come from the failure
// package so the same situation carries the same code whether it was seen
// while configuring a provider or while running a turn.
const (
	CodeOK                  = failure.CodeOK
	CodeQuota               = failure.CodeQuota
	CodeRateLimit           = failure.CodeRateLimit
	CodeAuthInvalid         = failure.CodeAuthInvalid
	CodeModelUnknown        = failure.CodeModelUnknown
	CodeContextExceeded     = failure.CodeContextExceeded
	CodeProtocolUnsupported = failure.CodeProtocolUnsupported
	CodeNetwork             = failure.CodeNetwork
	CodeEngineInternal      = failure.CodeEngineInternal
)

// Short Chinese phrases that go with the codes. They are kept short on
// purpose: they are shown next to a model name in a list, and they must
// never contain a secret, a full URL or the provider's raw answer.
var (
	messageOK                  = failure.Text(CodeOK)
	messageQuota               = failure.Text(CodeQuota)
	messageRateLimit           = failure.Text(CodeRateLimit)
	messageAuthInvalid         = failure.Text(CodeAuthInvalid)
	messageModelUnknown        = failure.Text(CodeModelUnknown)
	messageContextExceeded     = failure.Text(CodeContextExceeded)
	messageProtocolUnsupported = failure.Text(CodeProtocolUnsupported)
	messageNetwork             = failure.Text(CodeNetwork)
	messageEngineInternal      = failure.Text(CodeEngineInternal)
)

// Result is the outcome of one connectivity check.
type Result struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Probe checks the given models concurrently, one minimal request each.
//
// The returned map always holds an entry for every model that was asked
// about, including the ones that failed: the caller shows a per-model list
// and a missing row would look like a model that was never checked.
//
// A failing model never affects the others. A provider with one broken
// model and nine working ones is a working provider with a caveat, and the
// user can already see which one is broken.
func (d *Discoverer) Probe(ctx context.Context, baseURL, apiKey string, modelIDs []string) map[string]Result {
	results := make(map[string]Result, len(modelIDs))
	if len(modelIDs) == 0 {
		return results
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"

	ids := dedupe(modelIDs)

	// A plain channel is the semaphore: the project takes no extra
	// dependency for this, and four slots is all we need.
	sem := make(chan struct{}, maxProbeConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The slot is taken here, not when the goroutine starts, so a
			// queued model does not begin its request before the gateway
			// has room for it.
			sem <- struct{}{}
			defer func() { <-sem }()

			result := d.probeModel(ctx, endpoint, apiKey, id)

			mu.Lock()
			results[id] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

// probeRequest is the smallest request that still exercises a model end to
// end. It carries the model name and nothing else: no secret may end up in
// a request body, because bodies get logged by gateways.
type probeRequest struct {
	Model     string         `json:"model"`
	Messages  []probeMessage `json:"messages"`
	MaxTokens int            `json:"max_tokens"`
}

type probeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// probeModel sends one minimal request and turns the answer into a Result.
func (d *Discoverer) probeModel(ctx context.Context, endpoint, apiKey, modelID string) Result {
	encoded, err := json.Marshal(probeRequest{
		Model:     modelID,
		Messages:  []probeMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	if err != nil {
		return failureResult(CodeEngineInternal, failure.Text(CodeEngineInternal))
	}

	// The deadline starts here, not when the goroutine was spawned: a model
	// that waited in the queue must still get its full budget.
	callCtx, cancel := context.WithTimeout(ctx, d.probeTimeoutFor())
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return failureResult(CodeNetwork, failure.Text(CodeNetwork))
	}
	// The key travels in the header only.
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient().Do(req)
	if err != nil {
		// No status code: the request never reached an answer (DNS,
		// refused connection, TLS, timeout). That is a "cannot reach"
		// failure, not a refusal by the provider.
		return failureResult(CodeNetwork, failure.Text(CodeNetwork))
	}
	defer resp.Body.Close()

	body, _ := readBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, message := classify(resp.StatusCode, string(body))
		return failureResult(code, message)
	}
	return Result{OK: true, Code: CodeOK, Message: failure.Text(CodeOK)}
}

// failureResult builds a failed Result.
func failureResult(code, message string) Result {
	return Result{OK: false, Code: code, Message: message}
}

// dedupe removes repeated ids while keeping the order they were given in.
func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// classify maps a provider's answer onto a stable machine code and a short
// Chinese phrase. The shared classifier owns the rules so a probe and a
// runtime failure can never disagree about the same answer.
func classify(status int, body string) (code, message string) {
	if status == 0 {
		// No HTTP answer arrived at all: a refused connection, a timeout,
		// a DNS failure. The endpoint was never reached, which the shared
		// classifier cannot know from a status alone.
		return CodeNetwork, messageNetwork
	}
	code, message = failure.ClassifyHTTP(status, body)
	if code == failure.CodeModelUnknown {
		// Here the request named a model, so the missing thing is the
		// model — the generic wording would leave the user guessing whether
		// their address was wrong.
		message = messageModelUnknown
	}
	return code, message
}

// normalizeBody folds a provider's answer into a form the keyword search can
// use: lower case, with spaces and dashes turned into underscores. Gateways
// write the same reason in several ways ("rate limit", "rate-limit",
// "rate_limit_exceeded") and the table lists one spelling of each.
func normalizeBody(body string) string {
	lower := strings.ToLower(body)
	lower = strings.ReplaceAll(lower, " ", "_")
	lower = strings.ReplaceAll(lower, "-", "_")
	return lower
}

// contains reports whether the normalized body holds the keyword.
func contains(normalized, keyword string) bool {
	return strings.Contains(normalized, keyword)
}

// shapeError reports an endpoint that answered but not in the OpenAI shape.
// It is its own constructor because the classification cannot be read off a
// status code here: the endpoint did answer with 200.
func shapeError(op string, cause error) error {
	return &classifiedError{op: op, code: CodeProtocolUnsupported, message: failure.Text(CodeProtocolUnsupported), cause: cause}
}
