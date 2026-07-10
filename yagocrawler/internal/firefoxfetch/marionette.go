package firefoxfetch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// marionetteConn speaks Firefox's Marionette wire protocol. Each frame is
// "<decimal-length>:<json-payload>"; a command is the JSON array
// [0, msgID, name, params] and its reply is [1, msgID, error, result] with
// exactly one of error/result non-null. Firefox pushes one unsolicited
// greeting frame ({applicationType, marionetteProtocol}) the moment the socket
// opens. The fetcher drives one navigation at a time through a single
// long-lived session, so replies arrive in request order and the reply's msgID
// is a desynchronization check, not a demultiplexer.
type marionetteConn struct {
	conn  net.Conn
	buf   *bufio.Reader
	msgID int
}

func newMarionetteConn(conn net.Conn) *marionetteConn {
	return &marionetteConn{conn: conn, buf: bufio.NewReader(conn)}
}

// handshake consumes the greeting frame Firefox sends on connect; it must be
// read before the first command or that command's reply parse would instead
// see the greeting.
func (m *marionetteConn) handshake() error {
	if _, err := m.readFrame(); err != nil {
		return fmt.Errorf("read marionette greeting: %w", err)
	}
	return nil
}

func (m *marionetteConn) setDeadline(t time.Time) error {
	if err := m.conn.SetDeadline(t); err != nil {
		return fmt.Errorf("set marionette deadline: %w", err)
	}
	return nil
}

func (m *marionetteConn) close() error {
	if err := m.conn.Close(); err != nil {
		return fmt.Errorf("close marionette conn: %w", err)
	}
	return nil
}

// readFrame reads one length-prefixed frame. The length is ASCII decimal
// terminated by ':', then exactly that many payload bytes follow.
func (m *marionetteConn) readFrame() ([]byte, error) {
	length := 0
	sawDigit := false
	for {
		b, err := m.buf.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read marionette frame length: %w", err)
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return nil, fmt.Errorf("marionette frame: unexpected length byte %q", b)
		}
		length = length*10 + int(b-'0')
		sawDigit = true
	}
	if !sawDigit {
		return nil, fmt.Errorf("marionette frame: empty length prefix")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(m.buf, payload); err != nil {
		return nil, fmt.Errorf("marionette frame: read %d-byte payload: %w", length, err)
	}
	return payload, nil
}

func (m *marionetteConn) writeFrame(payload []byte) error {
	frame := append([]byte(strconv.Itoa(len(payload))+":"), payload...)
	if _, err := m.conn.Write(frame); err != nil {
		return fmt.Errorf("marionette frame: write: %w", err)
	}
	return nil
}

// command sends one Marionette command and returns its result payload. A
// non-null error slot becomes a Go error; a reply whose msgID does not match
// the request marks the stream desynchronized so the caller discards the
// session rather than trusting a misaligned reply.
func (m *marionetteConn) command(name string, params any) (json.RawMessage, error) {
	m.msgID++
	id := m.msgID
	request, err := json.Marshal([]any{0, id, name, params})
	if err != nil {
		return nil, fmt.Errorf("marshal marionette command %s: %w", name, err)
	}
	if err := m.writeFrame(request); err != nil {
		return nil, err
	}
	raw, err := m.readFrame()
	if err != nil {
		return nil, fmt.Errorf("marionette command %s: %w", name, err)
	}
	var reply []json.RawMessage
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode marionette reply for %s: %w", name, err)
	}
	if len(reply) != 4 {
		return nil, fmt.Errorf("marionette reply for %s had %d elements, want 4", name, len(reply))
	}
	var replyID int
	if err := json.Unmarshal(reply[1], &replyID); err != nil {
		return nil, fmt.Errorf("decode marionette reply id for %s: %w", name, err)
	}
	if replyID != id {
		return nil, fmt.Errorf(
			"marionette reply id %d for %s, want %d (stream desynchronized)",
			replyID,
			name,
			id,
		)
	}
	if len(reply[2]) > 0 && string(reply[2]) != "null" {
		return nil, fmt.Errorf(
			"marionette command %s failed: %s",
			name,
			marionetteErrorMessage(reply[2]),
		)
	}
	return reply[3], nil
}

// marionetteErrorMessage renders a Marionette error object as "error: message"
// for a readable Go error, falling back to the raw JSON if it is not the
// expected shape.
func marionetteErrorMessage(raw json.RawMessage) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &e); err == nil && (e.Error != "" || e.Message != "") {
		if e.Message != "" && e.Error != "" {
			return e.Error + ": " + e.Message
		}
		return e.Error + e.Message
	}
	return string(raw)
}

func (m *marionetteConn) newSession() error {
	_, err := m.command("WebDriver:NewSession", map[string]any{})
	return err
}

// setPageLoadTimeout bounds Firefox's own navigation and script waits so a
// hung page load returns a Marionette timeout error instead of blocking until
// the socket deadline cuts the stream.
func (m *marionetteConn) setPageLoadTimeout(d time.Duration) error {
	if d <= 0 {
		return nil
	}
	_, err := m.command("WebDriver:SetTimeouts", map[string]any{
		"pageLoad": d.Milliseconds(),
		"script":   d.Milliseconds(),
	})
	return err
}

func (m *marionetteConn) navigate(rawURL string) error {
	_, err := m.command("WebDriver:Navigate", map[string]any{"url": rawURL})
	return err
}

func (m *marionetteConn) currentURL() (string, error) {
	result, err := m.command("WebDriver:GetCurrentURL", map[string]any{})
	if err != nil {
		return "", err
	}
	return decodeValueString(result, "current url")
}

// executeScriptString runs JS in the current browsing context and decodes the
// script's return value as a string. Marionette wraps the snippet in a function
// body, so each script must `return` its value.
func (m *marionetteConn) executeScriptString(script string) (string, error) {
	result, err := m.command("WebDriver:ExecuteScript", map[string]any{
		"script": script,
		"args":   []any{},
	})
	if err != nil {
		return "", err
	}
	return decodeValueString(result, "script value")
}

func (m *marionetteConn) quit() error {
	_, err := m.command("Marionette:Quit", map[string]any{})
	return err
}

// decodeValueString unwraps the {"value": ...} envelope Marionette wraps around
// command results and decodes the inner value as a string.
func decodeValueString(raw json.RawMessage, what string) (string, error) {
	var wrap struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return "", fmt.Errorf("decode marionette %s envelope: %w", what, err)
	}
	var s string
	if err := json.Unmarshal(wrap.Value, &s); err != nil {
		return "", fmt.Errorf("marionette %s was not a string: %w", what, err)
	}
	return s, nil
}
