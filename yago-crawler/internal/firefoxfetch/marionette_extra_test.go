package firefoxfetch

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func writeRaw(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatalf("write raw %q: %v", s, err)
	}
}

func TestMarionetteReadFrameRejectsMalformedFrames(t *testing.T) {
	tests := []struct {
		name    string
		feed    func(t *testing.T, server net.Conn)
		wantErr string
	}{
		{
			name:    "non-digit length byte",
			feed:    func(t *testing.T, s net.Conn) { writeRaw(t, s, "Z") },
			wantErr: "unexpected length byte",
		},
		{
			name:    "empty length prefix",
			feed:    func(t *testing.T, s net.Conn) { writeRaw(t, s, ":") },
			wantErr: "empty length prefix",
		},
		{
			name:    "short payload",
			feed:    func(t *testing.T, s net.Conn) { writeRaw(t, s, "10:abc"); _ = s.Close() },
			wantErr: "read 10-byte payload",
		},
		{
			name:    "closed before length",
			feed:    func(_ *testing.T, s net.Conn) { _ = s.Close() },
			wantErr: "read marionette frame length",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, server := marionettePair(t)
			tc.feed(t, server)
			if _, err := client.readFrame(); err == nil ||
				!strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestMarionetteHandshakeErrorsWithoutGreeting(t *testing.T) {
	client, server := marionettePair(t)
	_ = server.Close()
	if err := client.handshake(); err == nil ||
		!strings.Contains(err.Error(), "read marionette greeting") {
		t.Fatalf("error = %v, want a greeting read failure", err)
	}
}

func TestMarionetteSetDeadlineErrorsOnClosedConn(t *testing.T) {
	client, _ := marionettePair(t)
	_ = client.conn.Close()
	if err := client.setDeadline(time.Now()); err == nil ||
		!strings.Contains(err.Error(), "set marionette deadline") {
		t.Fatalf("error = %v, want a set-deadline failure", err)
	}
}

func TestMarionetteCloseErrorsWhenAlreadyClosed(t *testing.T) {
	client, _ := marionettePair(t)
	if err := client.close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := client.close(); err == nil ||
		!strings.Contains(err.Error(), "close marionette conn") {
		t.Fatalf("error = %v, want a close failure", err)
	}
}

func TestMarionetteWriteFrameErrorsOnClosedConn(t *testing.T) {
	client, _ := marionettePair(t)
	_ = client.conn.Close()
	if err := client.writeFrame([]byte("x")); err == nil ||
		!strings.Contains(err.Error(), "marionette frame: write") {
		t.Fatalf("error = %v, want a write failure", err)
	}
}

func TestMarionetteCommandRejectsUnmarshalableParams(t *testing.T) {
	client, _ := marionettePair(t)
	if _, err := client.command("Cmd", make(chan int)); err == nil ||
		!strings.Contains(err.Error(), "marshal marionette command") {
		t.Fatalf("error = %v, want a marshal failure", err)
	}
}

func TestMarionetteCommandErrorsWhenWriteFails(t *testing.T) {
	client, _ := marionettePair(t)
	_ = client.conn.Close()
	if _, err := client.command("Cmd", map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "marionette frame: write") {
		t.Fatalf("error = %v, want a write failure", err)
	}
}

func TestMarionetteCommandErrorsWhenReplyUnreadable(t *testing.T) {
	client, server := marionettePair(t)
	go func() {
		reader := bufio.NewReader(server)
		_, _ = rawReadFrame(reader)
		_ = server.Close()
	}()
	if _, err := client.command("Cmd", map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "marionette command Cmd") {
		t.Fatalf("error = %v, want a reply read failure", err)
	}
}

func TestMarionetteCommandRejectsMalformedReplies(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		wantErr string
	}{
		{"not a json array", "notjson", "decode marionette reply"},
		{"wrong element count", "[1,2]", "want 4"},
		{"non-integer reply id", `[1,"x",null,null]`, "decode marionette reply id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, server := marionettePair(t)
			serveMarionette(server, func(_ []json.RawMessage) (string, bool) {
				return tc.reply, true
			})
			if err := client.handshake(); err != nil {
				t.Fatalf("handshake: %v", err)
			}
			if _, err := client.command("Cmd", map[string]any{}); err == nil ||
				!strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestMarionetteErrorMessageRendering(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"error and message", `{"error":"timeout","message":"slow"}`, "timeout: slow"},
		{"error only", `{"error":"timeout"}`, "timeout"},
		{"message only", `{"message":"slow"}`, "slow"},
		{"unexpected shape", `{"foo":"bar"}`, `{"foo":"bar"}`},
		{"not an object", `123`, "123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := marionetteErrorMessage(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeValueString(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{"string value", `{"value":"hello"}`, "hello", ""},
		{"non-object envelope", `123`, "", "envelope"},
		{"non-string value", `{"value":123}`, "", "was not a string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeValueString(json.RawMessage(tc.raw), "thing")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeValueString: %v", err)
			}
			if got != tc.want {
				t.Fatalf("value = %q, want %q", got, tc.want)
			}
		})
	}
}
