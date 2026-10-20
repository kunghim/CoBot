package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// replyServer answers each command with the result its method names and
// can push events on its own.
func replyServer(t *testing.T, results map[string]any, after func(c net.Conn)) string {
	t.Helper()
	return serveWS(t, func(c net.Conn, r *bufio.Reader) {
		for {
			op, payload, ok := readClientFrame(t, r)
			if !ok || op != wsOpText {
				return
			}
			var req struct {
				ID        int64  `json:"id"`
				Method    string `json:"method"`
				SessionID string `json:"sessionId"`
			}
			json.Unmarshal(payload, &req)
			var reply []byte
			switch req.Method {
			case "Test.fail":
				reply, _ = json.Marshal(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "no such thing"}})
			case "Test.drop":
				return
			case "Test.silent":
				continue
			default:
				reply, _ = json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{"method": req.Method, "session": req.SessionID, "extra": results[req.Method]}})
			}
			c.Write(serverFrame(true, wsOpText, reply))
			if after != nil && req.Method == "Test.event" {
				after(c)
			}
		}
	})
}

func TestCDPPairsRepliesEventsAndErrors(t *testing.T) {
	var mu sync.Mutex
	var events []string
	url := replyServer(t, map[string]any{"Page.navigate": "ok"}, func(c net.Conn) {
		event, _ := json.Marshal(map[string]any{"method": "Page.loadEventFired", "sessionId": "s1", "params": map[string]any{"timestamp": 1}})
		c.Write(serverFrame(true, wsOpText, event))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialCDP(ctx, url, func(session, method string, params json.RawMessage) {
		mu.Lock()
		events = append(events, session+"|"+method)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.close()

	var out struct {
		Method  string `json:"method"`
		Session string `json:"session"`
		Extra   string `json:"extra"`
	}
	if err := conn.call(ctx, "s1", "Page.navigate", map[string]any{"url": "about:blank"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Method != "Page.navigate" || out.Session != "s1" || out.Extra != "ok" {
		t.Fatalf("reply = %+v", out)
	}
	// A command the browser refuses is an error carrying its message.
	var refused *cdpError
	if err := conn.call(ctx, "", "Test.fail", nil, nil); err == nil || !asCDPError(err, &refused) || refused.Message != "no such thing" {
		t.Fatalf("refused command = %v", err)
	}
	// An event arrives on its own, with the session it belongs to.
	if err := conn.call(ctx, "", "Test.event", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(events) == 1 })
	if events[0] != "s1|Page.loadEventFired" {
		t.Fatalf("events = %v", events)
	}
}

func asCDPError(err error, target **cdpError) bool {
	e, ok := err.(*cdpError)
	if ok {
		*target = e
	}
	return ok
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestCDPCallsEndWhenTheContextExpiresOrTheBrowserGoesAway(t *testing.T) {
	url := replyServer(t, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialCDP(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.close()

	short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if err := conn.call(short, "", "Test.silent", nil, nil); err == nil || short.Err() == nil {
		t.Fatalf("a call nobody answered returned %v", err)
	}
	if !conn.alive() {
		t.Fatal("a timed-out call closed the connection")
	}
	// The browser dropping the connection fails the call and every later one.
	if err := conn.call(ctx, "", "Test.drop", nil, nil); err == nil {
		t.Fatal("a call on a dropped connection succeeded")
	}
	waitFor(t, func() bool { return !conn.alive() })
	if err := conn.call(ctx, "", "Page.enable", nil, nil); err == nil {
		t.Fatal("a call after the connection ended succeeded")
	}
}
