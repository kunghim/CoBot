package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A minimal RFC 6455 client, enough for the browser's debugging socket: the
// handshake, text messages in any number of fragments, ping/pong and close.
// Chrome speaks to a loopback peer only, so there is no TLS and no
// compression; keeping it here avoids a dependency for a few hundred lines.

const (
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA

	// A screenshot or a large page snapshot is the biggest message the
	// browser sends; anything past this is a broken peer.
	wsMaxMessage = 64 << 20
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

var errWSClosed = errors.New("websocket closed")

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader

	wmu    sync.Mutex
	closed bool
}

// dialWS connects to a ws:// URL. No Origin header is sent: Chrome only
// accepts a debugging connection without one or from an allowed origin.
func dialWS(ctx context.Context, rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket address %q", rawURL)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	request := "GET " + path + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, err
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(key + wsGUID))
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		!strings.EqualFold(resp.Header.Get("Sec-WebSocket-Accept"), base64.StdEncoding.EncodeToString(sum[:])) {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake refused: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return &wsConn{conn: conn, r: r}, nil
}

// writeText sends one masked text frame.
func (c *wsConn) writeText(payload []byte) error {
	return c.writeFrame(wsOpText, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return errWSClosed
	}
	header := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n <= 0xFFFF:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}

// readMessage returns the next complete text or binary message, answering
// pings on the way. It returns errWSClosed when the peer closes.
func (c *wsConn) readMessage() ([]byte, error) {
	var message []byte
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(c.r, head); err != nil {
			return nil, err
		}
		fin := head[0]&0x80 != 0
		opcode := head[0] & 0x0F
		masked := head[1]&0x80 != 0
		length := uint64(head[1] & 0x7F)
		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.r, ext); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.r, ext); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext)
		}
		if length > wsMaxMessage || uint64(len(message))+length > wsMaxMessage {
			return nil, errors.New("websocket message too large")
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.r, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case wsOpPing:
			_ = c.writeFrame(wsOpPong, payload)
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			_ = c.writeFrame(wsOpClose, nil)
			return nil, errWSClosed
		case wsOpText, wsOpBinary:
			message = append(message[:0], payload...)
		case wsOpContinuation:
			message = append(message, payload...)
		default:
			return nil, fmt.Errorf("unexpected websocket opcode %d", opcode)
		}
		if fin {
			return message, nil
		}
	}
}

// close ends the connection; further writes fail.
func (c *wsConn) close() {
	c.wmu.Lock()
	if !c.closed {
		c.closed = true
		_ = c.conn.Close()
	}
	c.wmu.Unlock()
}
