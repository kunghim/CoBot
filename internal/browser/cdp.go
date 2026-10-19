package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// cdpConn is a Chrome DevTools Protocol connection: numbered commands that
// get a response, and events that arrive on their own. Commands for a page
// carry its session id; the browser itself uses the empty one.
type cdpConn struct {
	ws      *wsConn
	nextID  atomic.Int64
	onEvent func(sessionID, method string, params json.RawMessage)

	mu      sync.Mutex
	pending map[int64]chan cdpReply
	err     error
	done    chan struct{}
}

type cdpReply struct {
	result json.RawMessage
	err    error
}

// cdpError is a command the browser refused.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// dialCDP connects to the browser's debugging socket. onEvent runs on the
// reading goroutine, so it must only record the event and return.
func dialCDP(ctx context.Context, wsURL string, onEvent func(sessionID, method string, params json.RawMessage)) (*cdpConn, error) {
	ws, err := dialWS(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	c := &cdpConn{ws: ws, onEvent: onEvent, pending: map[int64]chan cdpReply{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *cdpConn) readLoop() {
	for {
		message, err := c.ws.readMessage()
		if err != nil {
			c.fail(err)
			return
		}
		var frame struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			Result    json.RawMessage `json:"result"`
			Error     *cdpError       `json:"error"`
			SessionID string          `json:"sessionId"`
		}
		if json.Unmarshal(message, &frame) != nil {
			continue
		}
		if frame.ID != 0 {
			c.mu.Lock()
			ch := c.pending[frame.ID]
			delete(c.pending, frame.ID)
			c.mu.Unlock()
			if ch != nil {
				if frame.Error != nil {
					ch <- cdpReply{err: frame.Error}
				} else {
					ch <- cdpReply{result: frame.Result}
				}
			}
			continue
		}
		if frame.Method != "" && c.onEvent != nil {
			c.onEvent(frame.SessionID, frame.Method, frame.Params)
		}
	}
}

// fail ends the connection: every waiting command gets the error.
func (c *cdpConn) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
		close(c.done)
	}
	for id, ch := range c.pending {
		ch <- cdpReply{err: err}
		delete(c.pending, id)
	}
	c.mu.Unlock()
	c.ws.close()
}

// call runs one command and decodes its result into out (nil to ignore it).
func (c *cdpConn) call(ctx context.Context, sessionID, method string, params any, out any) error {
	id := c.nextID.Add(1)
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if sessionID != "" {
		request["sessionId"] = sessionID
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	reply := make(chan cdpReply, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.pending[id] = reply
	c.mu.Unlock()
	if err := c.ws.writeText(payload); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return r.err
		}
		if out != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case <-c.done:
		return c.connErr()
	}
}

func (c *cdpConn) connErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		return errors.New("browser connection closed")
	}
	return c.err
}

func (c *cdpConn) close() { c.fail(errWSClosed) }

// alive reports whether the connection still works.
func (c *cdpConn) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
