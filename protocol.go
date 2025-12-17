package borego

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
)

const (
	// ControlPort is the TCP port used by the bore control channel.
	ControlPort = 7835

	// MaxFrameLength caps the length of a JSON frame (excluding the trailing null byte).
	MaxFrameLength = 256

	// NetworkTimeout defines the timeout for dialing and initial protocol steps.
	NetworkTimeout = 3 * time.Second
)

// Delimited wraps a TCP connection with null-delimited JSON framing.
type Delimited struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

// NewDelimited constructs a framed connection.
func NewDelimited(conn net.Conn) *Delimited {
	return &Delimited{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
}

// SendJSON marshals and sends a JSON value terminated by a null byte.
func (d *Delimited) SendJSON(v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(payload) > MaxFrameLength {
		return fmt.Errorf("frame too large: %d bytes", len(payload))
	}
	if err := d.conn.SetWriteDeadline(time.Now().Add(NetworkTimeout)); err != nil {
		return err
	}
	if _, err := d.w.Write(payload); err != nil {
		return err
	}
	if err := d.w.WriteByte(0); err != nil {
		return err
	}
	return d.w.Flush()
}

// RecvFrame reads the next null-delimited JSON frame.
// The returned bool is false on clean EOF.
func (d *Delimited) RecvFrame(timeout bool) ([]byte, bool, error) {
	if timeout {
		if err := d.conn.SetReadDeadline(time.Now().Add(NetworkTimeout)); err != nil {
			return nil, false, err
		}
	} else {
		// Clear deadline for long-lived reads.
		if err := d.conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, false, err
		}
	}

	frame, err := d.r.ReadBytes(0)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, false, nil
		}
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			return nil, false, fmt.Errorf("timed out waiting for message: %w", err)
		}
		return nil, false, err
	}
	if len(frame) == 0 {
		return nil, false, fmt.Errorf("empty frame")
	}
	frame = bytes.TrimSuffix(frame, []byte{0})
	if len(frame) > MaxFrameLength {
		return nil, false, fmt.Errorf("frame too large: %d bytes", len(frame))
	}
	return frame, true, nil
}

// BufferedData drains any data currently buffered by the reader.
func (d *Delimited) BufferedData() ([]byte, error) {
	buffered := d.r.Buffered()
	if buffered == 0 {
		return nil, nil
	}
	peeked, err := d.r.Peek(buffered)
	if err != nil {
		return nil, err
	}
	// Discard to advance the buffer so the underlying conn can be used directly.
	if _, err := d.r.Discard(buffered); err != nil {
		return nil, err
	}
	return append([]byte(nil), peeked...), nil
}

// Close closes the underlying connection.
func (d *Delimited) Close() error {
	return d.conn.Close()
}

// RawConn exposes the underlying connection for proxying.
func (d *Delimited) RawConn() net.Conn {
	return d.conn
}

// ServerMessageKind enumerates message types sent from the server.
type ServerMessageKind string

const (
	ServerHello      ServerMessageKind = "hello"
	ServerChallenge  ServerMessageKind = "challenge"
	ServerHeartbeat  ServerMessageKind = "heartbeat"
	ServerConnection ServerMessageKind = "connection"
	ServerError      ServerMessageKind = "error"
)

// ServerMessage is a parsed message sent from the bore server.
type ServerMessage struct {
	Kind      ServerMessageKind
	Port      uint16
	ID        uuid.UUID
	ErrorText string
}

func decodeServerMessage(data []byte) (ServerMessage, error) {
	var unit string
	if err := json.Unmarshal(data, &unit); err == nil {
		if unit == "Heartbeat" {
			return ServerMessage{Kind: ServerHeartbeat}, nil
		}
		return ServerMessage{}, fmt.Errorf("unexpected unit message: %s", unit)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return ServerMessage{}, fmt.Errorf("invalid server message: %w", err)
	}
	if len(obj) != 1 {
		return ServerMessage{}, fmt.Errorf("expected single-field message, got %d keys", len(obj))
	}
	for k, v := range obj {
		switch k {
		case "Hello":
			var port uint16
			if err := json.Unmarshal(v, &port); err != nil {
				return ServerMessage{}, fmt.Errorf("invalid hello message: %w", err)
			}
			return ServerMessage{Kind: ServerHello, Port: port}, nil
		case "Challenge":
			var raw string
			if err := json.Unmarshal(v, &raw); err != nil {
				return ServerMessage{}, fmt.Errorf("invalid challenge message: %w", err)
			}
			id, err := uuid.Parse(raw)
			if err != nil {
				return ServerMessage{}, fmt.Errorf("invalid challenge uuid: %w", err)
			}
			return ServerMessage{Kind: ServerChallenge, ID: id}, nil
		case "Connection":
			var raw string
			if err := json.Unmarshal(v, &raw); err != nil {
				return ServerMessage{}, fmt.Errorf("invalid connection message: %w", err)
			}
			id, err := uuid.Parse(raw)
			if err != nil {
				return ServerMessage{}, fmt.Errorf("invalid connection uuid: %w", err)
			}
			return ServerMessage{Kind: ServerConnection, ID: id}, nil
		case "Error":
			var msg string
			if err := json.Unmarshal(v, &msg); err != nil {
				return ServerMessage{}, fmt.Errorf("invalid error message: %w", err)
			}
			return ServerMessage{Kind: ServerError, ErrorText: msg}, nil
		default:
			return ServerMessage{}, fmt.Errorf("unknown message kind: %s", k)
		}
	}
	return ServerMessage{}, fmt.Errorf("unreachable")
}

func encodeHello(port uint16) map[string]interface{} {
	return map[string]interface{}{"Hello": port}
}

func encodeAuthenticate(tag string) map[string]interface{} {
	return map[string]interface{}{"Authenticate": tag}
}

func encodeAccept(id uuid.UUID) map[string]interface{} {
	return map[string]interface{}{"Accept": id}
}

// RecvServer reads and decodes a server message.
func (d *Delimited) RecvServer(timeout bool) (ServerMessage, bool, error) {
	frame, ok, err := d.RecvFrame(timeout)
	if err != nil || !ok {
		return ServerMessage{}, ok, err
	}
	msg, err := decodeServerMessage(frame)
	return msg, ok, err
}
