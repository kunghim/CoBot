// Package codex adapts the Codex app-server (JSON-RPC over stdio) to the
// engine.Server interface. It owns process lifecycle and protocol plumbing
// only: no business database, no delivery queue, no approval policy.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// RPCError is a JSON-RPC error returned by the app-server.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("codex rpc %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type pendingCall struct {
	result chan rpcMessage
	done   bool
}

// InboundRequest is a server→client request (native approvals, tool calls).
// ID keeps the original JSON form (number or string) for exact routing.
type InboundRequest struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
}

// Client speaks line-delimited JSON-RPC with an app-server process.
type Client struct {
	stdin   io.WriteCloser
	nextID  atomic.Int64
	mu      sync.Mutex
	writeMu sync.Mutex // guards stdin writes; never held while waiting on responses
	pending map[string]*pendingCall
	closed  chan struct{}
	exitErr error
	wg      sync.WaitGroup

	onRequest func(InboundRequest)
	onNotify  func(method string, params json.RawMessage)
	onExit    func(err error)
	logger    *slog.Logger
}

// ClientHooks are the callbacks a Client delivers inbound traffic through.
type ClientHooks struct {
	OnRequest func(InboundRequest)
	OnNotify  func(method string, params json.RawMessage)
	OnExit    func(err error)
	Logger    *slog.Logger
}

// NewClient wires the streams of a spawned app-server and starts reading.
func NewClient(stdin io.WriteCloser, stdout io.Reader, hooks ClientHooks) *Client {
	if hooks.Logger == nil {
		hooks.Logger = slog.Default()
	}
	c := &Client{
		stdin:     stdin,
		pending:   map[string]*pendingCall{},
		closed:    make(chan struct{}),
		onRequest: hooks.OnRequest,
		onNotify:  hooks.OnNotify,
		onExit:    hooks.OnExit,
		logger:    hooks.Logger,
	}
	c.nextID.Store(1)
	c.wg.Add(1)
	go c.readLoop(stdout)
	return c
}

func (c *Client) readLoop(stdout io.Reader) {
	defer c.wg.Done()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.logger.Warn("codex: dropping malformed line", "error", err)
			continue
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			// Server-initiated request (approval...).
			if c.onRequest != nil {
				c.onRequest(InboundRequest{ID: msg.ID, Method: msg.Method, Params: msg.Params})
			}
		case msg.Method != "":
			if c.onNotify != nil {
				c.onNotify(msg.Method, msg.Params)
			}
		default:
			c.resolve(msg)
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.fail(err)
}

func (c *Client) resolve(msg rpcMessage) {
	key := string(msg.ID)
	c.mu.Lock()
	p, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
		p.done = true
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	p.result <- msg
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.exitErr == nil {
		c.exitErr = err
	}
	for key, p := range c.pending {
		if !p.done {
			p.done = true
			p.result <- rpcMessage{Error: &RPCError{Message: err.Error()}}
		}
		delete(c.pending, key)
	}
	c.mu.Unlock()
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	if c.onExit != nil {
		c.onExit(err)
	}
}

// Closed is signalled when the process side disappears.
func (c *Client) Closed() <-chan struct{} { return c.closed }

// Call performs a request/response round trip bounded by ctx.
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1) - 1
	payload := map[string]any{"id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}
	p := &pendingCall{result: make(chan rpcMessage, 1)}
	key := fmt.Sprintf("%d", id)
	c.mu.Lock()
	if c.exitErr != nil {
		err := c.exitErr
		c.mu.Unlock()
		return fmt.Errorf("codex app-server unavailable: %w", err)
	}
	c.pending[key] = p
	c.mu.Unlock()

	if err := c.write(data); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		if pp, ok := c.pending[key]; ok && !pp.done {
			pp.done = true
			delete(c.pending, key)
		}
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.closed:
		return fmt.Errorf("codex app-server unavailable: %w", c.exitErr)
	case resp := <-p.result:
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
}

// Reply answers an inbound request with the given result object.
func (c *Client) Reply(id json.RawMessage, result any) error {
	data, err := json.Marshal(map[string]any{"id": json.RawMessage(id), "result": result})
	if err != nil {
		return fmt.Errorf("encode reply: %w", err)
	}
	return c.write(data)
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	payload := map[string]any{"method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode notify %s: %w", method, err)
	}
	return c.write(data)
}

func (c *Client) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitErr != nil {
		return fmt.Errorf("codex app-server unavailable: %w", c.exitErr)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to codex app-server: %w", err)
	}
	return nil
}

// Close ends the stdin stream; the caller still owns process termination.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.exitErr != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.stdin.Close()
}

// Wait blocks until the read loop observed process exit.
func (c *Client) Wait() { c.wg.Wait() }

// Err returns the exit error, if any.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}
