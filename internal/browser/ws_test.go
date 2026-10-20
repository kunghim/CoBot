package browser

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serveWS accepts one connection, completes the handshake the way Chrome
// does and hands the raw connection to script.
func serveWS(t *testing.T, script func(c net.Conn, r *bufio.Reader)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		req, err := http.ReadRequest(r)
		if err != nil {
			return
		}
		sum := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + wsGUID))
		io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+
			base64.StdEncoding.EncodeToString(sum[:])+"\r\n\r\n")
		script(c, r)
	}()
	return "ws://" + ln.Addr().String() + "/devtools/browser/test"
}

// readClientFrame reads one masked client frame.
// A closed connection (the test is over) reads as ok == false, quietly.
func readClientFrame(t *testing.T, r *bufio.Reader) (opcode byte, payload []byte, ok bool) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, nil, false
	}
	if head[1]&0x80 == 0 {
		t.Error("client frame was not masked")
	}
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		io.ReadFull(r, ext)
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		io.ReadFull(r, ext)
		length = binary.BigEndian.Uint64(ext)
	}
	mask := make([]byte, 4)
	io.ReadFull(r, mask)
	payload = make([]byte, length)
	io.ReadFull(r, payload)
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return head[0] & 0x0F, payload, true
}

// serverFrame builds an unmasked server frame.
func serverFrame(fin bool, opcode byte, payload []byte) []byte {
	first := opcode
	if fin {
		first |= 0x80
	}
	frame := []byte{first}
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, byte(n))
	case n <= 0xFFFF:
		frame = append(frame, 126, byte(n>>8), byte(n))
	default:
		frame = append(frame, 127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	return append(frame, payload...)
}

func dialTest(t *testing.T, url string) *wsConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := dialWS(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	return c
}

func TestWSEchoesMessagesOfEverySizeClass(t *testing.T) {
	url := serveWS(t, func(c net.Conn, r *bufio.Reader) {
		for {
			op, payload, ok := readClientFrame(t, r)
			if !ok || op != wsOpText {
				return
			}
			c.Write(serverFrame(true, wsOpText, payload))
		}
	})
	conn := dialTest(t, url)
	for _, size := range []int{0, 5, 125, 126, 300, 65535, 65536, 200000} {
		message := []byte(strings.Repeat("x", size))
		if err := conn.writeText(message); err != nil {
			t.Fatalf("write %d: %v", size, err)
		}
		got, err := conn.readMessage()
		if err != nil || string(got) != string(message) {
			t.Fatalf("size %d echoed %d bytes, %v", size, len(got), err)
		}
	}
}

func TestWSJoinsFragmentsAndAnswersPingsBetweenThem(t *testing.T) {
	pong := make(chan []byte, 1)
	url := serveWS(t, func(c net.Conn, r *bufio.Reader) {
		c.Write(serverFrame(false, wsOpText, []byte("hel")))
		c.Write(serverFrame(true, wsOpPing, []byte("are-you-there")))
		c.Write(serverFrame(false, wsOpContinuation, []byte("lo, ")))
		c.Write(serverFrame(true, wsOpContinuation, []byte("world")))
		op, payload, ok := readClientFrame(t, r)
		if ok && op == wsOpPong {
			pong <- payload
		}
	})
	conn := dialTest(t, url)
	got, err := conn.readMessage()
	if err != nil || string(got) != "hello, world" {
		t.Fatalf("message = %q, %v", got, err)
	}
	select {
	case payload := <-pong:
		if string(payload) != "are-you-there" {
			t.Fatalf("pong payload = %q", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the ping was never answered")
	}
}

func TestWSReportsCloseAndRefusedHandshakes(t *testing.T) {
	url := serveWS(t, func(c net.Conn, r *bufio.Reader) {
		c.Write(serverFrame(true, wsOpClose, nil))
	})
	conn := dialTest(t, url)
	if _, err := conn.readMessage(); !errors.Is(err, errWSClosed) {
		t.Fatalf("read after close = %v", err)
	}
	conn.close()
	if err := conn.writeText([]byte("late")); !errors.Is(err, errWSClosed) {
		t.Fatalf("write after close = %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		http.ReadRequest(bufio.NewReader(c))
		io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := dialWS(ctx, "ws://"+ln.Addr().String()+"/x"); err == nil {
		t.Fatal("a refused handshake connected")
	}
	if _, err := dialWS(ctx, "wss://example.invalid/x"); err == nil {
		t.Fatal("a wss address was accepted")
	}
}
