// Package provider talks to OpenAI-compatible model providers.
//
// It answers the two questions a user has while setting a provider up: which
// models does this endpoint offer (Discover), and does the model I picked
// actually answer (Probe). Nothing else happens here - no storage, no engine,
// no UI - so the package stays usable from the API layer, the desktop shell
// and the tests alike.
//
// The codes and phrases produced by classify are a shared vocabulary: the
// failure package uses the same ones, so both sides of the product name the
// same problem the same way.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxBodyBytes caps how much of a provider's answer we read. A failure
	// is decided by a handful of keywords, and an unbounded read lets a
	// hostile or broken endpoint stream forever.
	maxBodyBytes = 8 << 10

	// maxModelListBytes caps the model list. It is far larger than the
	// answer to a failure check, and it has to be: a real vendor lists
	// hundreds of models, and a gateway's catalogue ran past twenty
	// kilobytes. A truncated list is not a small list — it is a body that
	// no longer parses, which used to be reported as an endpoint that
	// speaks no known protocol.
	maxModelListBytes = 1 << 20

	// defaultDiscoverTimeout bounds one whole Discover call: listing models
	// is a single request and must not hold the UI hostage.
	defaultDiscoverTimeout = 20 * time.Second

	// defaultProbeTimeout bounds one model's probe request.
	defaultProbeTimeout = 15 * time.Second

	// maxProbeConcurrency is how many models are probed at the same time.
	// Four is enough to feel instant on a typical catalogue and small
	// enough that a user with thirty models does not hammer the gateway.
	maxProbeConcurrency = 4

	// clientTimeout is the backstop on the HTTP client. It is deliberately
	// larger than the per-call deadlines so the context deadline, which
	// carries the better error, is what actually fires.
	clientTimeout = 60 * time.Second
)

// Discoverer talks to a provider's OpenAI-compatible endpoint.
//
// It holds no per-call state, so one Discoverer can serve concurrent
// requests. The fields are unexported on purpose: the caller configures a
// Discoverer through New and only reads results back.
type Discoverer struct {
	client *http.Client
	// timeout bounds a whole Discover call.
	timeout time.Duration
	// probeTimeout bounds a single model's probe request.
	probeTimeout time.Duration
}

// New returns a discoverer with sane timeouts.
func New() *Discoverer {
	return &Discoverer{
		client:       &http.Client{Timeout: clientTimeout},
		timeout:      defaultDiscoverTimeout,
		probeTimeout: defaultProbeTimeout,
	}
}

// Discover lists the model ids the endpoint offers. It returns the ids it
// could read; an endpoint that answers with a shape we do not understand is
// an error, not an empty list.
//
// The empty list is reserved for an endpoint that answered in the expected
// shape and offered nothing, because "this gateway has no models" and "this
// gateway is not an OpenAI-compatible gateway" are different problems with
// different fixes.
func (d *Discoverer) Discover(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if err := checkBaseURL(baseURL); err != nil {
		return nil, classifyError("discover", 0, "", err)
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"

	callCtx, cancel := context.WithTimeout(ctx, d.discoverTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, classifyError("discover", 0, "", err)
	}
	// The key travels in the header only. Nothing that could be logged or
	// cached with the request carries it.
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := d.httpClient().Do(req)
	if err != nil {
		// No status code: the request never reached an answer (DNS,
		// refused connection, TLS, timeout).
		return nil, classifyError("discover", 0, "", err)
	}
	defer resp.Body.Close()

	body, err := readBodyLimit(resp.Body, maxModelListBytes)
	if err != nil {
		return nil, classifyError("discover", 0, "", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyError("discover", resp.StatusCode, string(body), fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	ids, err := parseModelList(body)
	if err != nil {
		// The endpoint answered with 200 but not in the OpenAI shape, so
		// the status code says nothing useful about what went wrong.
		return nil, shapeError("discover", err)
	}
	return ids, nil
}

// parseModelList reads an OpenAI model list and returns the ids in the order
// the endpoint listed them.
//
// Elements are decoded one by one so that a single odd entry does not throw
// away the whole catalogue: the caller wants every id it can read.
func parseModelList(body []byte) ([]string, error) {
	var payload struct {
		Data *[]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("response is not JSON: %w", err)
	}
	if payload.Data == nil {
		return nil, errors.New("response has no model list")
	}

	entries := *payload.Data
	ids := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, raw := range entries {
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			// Not an object: nothing to read from it.
			continue
		}
		// id first, model as the fallback: some gateways only fill one.
		id := stringField(fields, "id")
		if id == "" {
			id = stringField(fields, "model")
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// stringField reads a trimmed string field, treating any other type as
// absent.
func stringField(fields map[string]any, key string) string {
	value, ok := fields[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// readBody reads at most maxBodyBytes of a response body.
func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxBodyBytes))
}

// readBodyLimit reads at most the given number of bytes of a response body.
func readBodyLimit(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

// httpClient returns the client to use, falling back to the default one for a
// zero-value Discoverer.
func (d *Discoverer) httpClient() *http.Client {
	if d.client != nil {
		return d.client
	}
	return http.DefaultClient
}

// discoverTimeout returns the deadline for one Discover call.
func (d *Discoverer) discoverTimeout() time.Duration {
	if d.timeout > 0 {
		return d.timeout
	}
	return defaultDiscoverTimeout
}

// probeTimeoutFor returns the deadline for one model's probe request.
func (d *Discoverer) probeTimeoutFor() time.Duration {
	if d.probeTimeout > 0 {
		return d.probeTimeout
	}
	return defaultProbeTimeout
}

// classifyError builds the error a caller sees for a failed call.
//
// The message carries only the stable code, the short phrase and the
// operation. The cause is attached for errors.Is/As and for logs, but it is
// kept out of the printed message on purpose: a transport error names the
// full endpoint URL, and the provider's own answer may quote the request
// back. Neither belongs in a line the user reads.
func classifyError(op string, status int, body string, cause error) error {
	code, message := classify(status, body)
	return &classifiedError{op: op, code: code, message: message, cause: cause}
}

// classifiedError is the error type every failed call returns.
type classifiedError struct {
	op      string
	code    string
	message string
	cause   error
}

func (e *classifiedError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.op, e.code, e.message)
}

// Unwrap exposes the cause to errors.Is and errors.As without printing it.
func (e *classifiedError) Unwrap() error { return e.cause }

// Reason is the part of a failed call's error a person reads: "密钥无效"
// rather than "discover: auth_invalid: 密钥无效". The operation and the
// machine code stay in the log line.
func Reason(err error) string {
	var classified *classifiedError
	if errors.As(err, &classified) {
		return classified.message
	}
	return err.Error()
}

// checkBaseURL accepts only an http or https address with a host. The
// address is the user's own choice of gateway, so any host is allowed, but
// nothing else is: no file:, no bare path, no scheme-less string.
func checkBaseURL(baseURL string) error {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return fmt.Errorf("endpoint address: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("endpoint address must be an http or https URL")
	}
	return nil
}
