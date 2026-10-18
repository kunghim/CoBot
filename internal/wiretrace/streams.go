package wiretrace

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"sync/atomic"
)

// Lines records a byte stream one line at a time (without the newline), as
// the stream is written; a trailing partial line is recorded by Flush.
type Lines struct {
	rec                *Recorder
	channel, dir, peer string
	mu                 sync.Mutex
	buf                []byte
}

// NewLines records the stream under channel/dir/peer.
func NewLines(rec *Recorder, channel, dir, peer string) *Lines {
	return &Lines{rec: rec, channel: channel, dir: dir, peer: peer}
}

// SetPeer names the other end once it is known (a process's pid).
func (l *Lines) SetPeer(peer string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.peer = peer
}

// Write records every complete line in p; it never fails.
func (l *Lines) Write(p []byte) (int, error) {
	if l == nil || l.rec == nil {
		return len(p), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.rec.Record(l.channel, l.dir, l.peer, bytes.TrimSuffix(l.buf[:i], []byte("\r")))
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

// Flush records what is left of an unterminated last line.
func (l *Lines) Flush() {
	if l == nil || l.rec == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.rec.Record(l.channel, l.dir, l.peer, l.buf)
		l.buf = nil
	}
}

// TeeReader passes r through unchanged while recording it line by line.
func TeeReader(r io.Reader, lines *Lines) io.Reader {
	if lines == nil || lines.rec == nil {
		return r
	}
	return io.TeeReader(r, lines)
}

// TeeWriter passes writes to w unchanged while recording them line by line.
func TeeWriter(w io.Writer, lines *Lines) io.Writer {
	if lines == nil || lines.rec == nil {
		return w
	}
	return io.MultiWriter(w, lines)
}

var httpSeq atomic.Int64

// Transport records every request a client sends and the response it gets,
// headers and bodies in full, paired by a peer id.
type Transport struct {
	Rec     *Recorder
	Channel string
	Base    http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if !t.Rec.Active() {
		return base.RoundTrip(req)
	}
	peer := fmt.Sprintf("%s#%d", t.Rec.proc, httpSeq.Add(1))
	if dump, err := httputil.DumpRequestOut(req, true); err == nil {
		t.Rec.Record(t.Channel, "request", peer, dump)
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		t.Rec.Record(t.Channel, "error", peer, []byte(err.Error()))
		return nil, err
	}
	if dump, err := httputil.DumpResponse(resp, true); err == nil {
		t.Rec.Record(t.Channel, "response", peer, dump)
	}
	return resp, nil
}

// Middleware records every request the server receives and the response it
// writes. A streamed response (server-sent events) is recorded frame by
// frame as it is written, so long-lived streams show up while they run.
func Middleware(rec *Recorder, next http.Handler) http.Handler {
	if rec == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rec.Active() {
			next.ServeHTTP(w, r)
			return
		}
		peer := fmt.Sprintf("http#%d", httpSeq.Add(1))
		if dump, err := httputil.DumpRequest(r, true); err == nil {
			rec.Record("http", "request", peer, dump)
		}
		cw := &captureWriter{ResponseWriter: w, rec: rec, peer: peer, status: http.StatusOK}
		next.ServeHTTP(cw, r)
		if cw.streaming {
			rec.Record("http", "response-end", peer, nil)
			return
		}
		rec.Record("http", "response", peer, cw.dump(cw.body.Bytes()))
	})
}

type captureWriter struct {
	http.ResponseWriter
	rec         *Recorder
	peer        string
	status      int
	wroteHeader bool
	streaming   bool
	body        bytes.Buffer
}

func (c *captureWriter) WriteHeader(status int) {
	if !c.wroteHeader {
		c.wroteHeader = true
		c.status = status
		c.streaming = strings.HasPrefix(c.Header().Get("Content-Type"), "text/event-stream")
		if c.streaming {
			c.rec.Record("http", "response-head", c.peer, c.dump(nil))
		}
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if c.streaming {
		c.rec.Record("http", "response-chunk", c.peer, p)
	} else {
		c.body.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

func (c *captureWriter) Flush() {
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (c *captureWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// dump renders the status line, headers and body as they go out.
func (c *captureWriter) dump(body []byte) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "HTTP/1.1 %d %s\r\n", c.status, http.StatusText(c.status))
	_ = c.Header().Write(&out)
	out.WriteString("\r\n")
	out.Write(body)
	return out.Bytes()
}
