package firefoxfetch

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeMarionetteServer starts a scripted Marionette server on a fresh loopback
// port (greeting first, then responder per command) and returns the port, so
// the real launch/connect code can be driven without a browser.
func fakeMarionetteServer(
	t *testing.T,
	responder func(request []json.RawMessage) (reply string, keepGoing bool),
) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serveMarionette(conn, responder)
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestFirefoxBinaryPrefersExplicitPath(t *testing.T) {
	got, err := firefoxBinary("/opt/custom/firefox")
	if err != nil {
		t.Fatalf("firefoxBinary: %v", err)
	}
	if got != "/opt/custom/firefox" {
		t.Fatalf("binary = %q, want the explicit path", got)
	}
}

func TestFirefoxBinaryDiscardsChromiumPath(t *testing.T) {
	restore := firefoxBinaries
	t.Cleanup(func() { firefoxBinaries = restore })
	firefoxBinaries = []string{"sh"}

	got, err := firefoxBinary("/usr/bin/chromium")
	if err != nil {
		t.Fatalf("firefoxBinary: %v", err)
	}
	if strings.Contains(got, "chromium") {
		t.Fatalf("binary = %q, want a discovered Firefox stand-in, not the chromium path", got)
	}
}

func TestLooksLikeChromium(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/usr/bin/chromium", true},
		{"/usr/bin/chromium-browser", true},
		{"/usr/bin/chrome", true},
		{"/opt/google/chrome/google-chrome", true},
		{"/usr/bin/firefox-esr", false},
		{"/usr/bin/firefox", false},
		{"/opt/custom/firefox", false},
		{"", false},
	} {
		if got := LooksLikeChromium(tc.path); got != tc.want {
			t.Errorf("LooksLikeChromium(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestFirefoxBinaryErrorsWhenNoneOnPath(t *testing.T) {
	restore := firefoxBinaries
	t.Cleanup(func() { firefoxBinaries = restore })
	firefoxBinaries = []string{"yago-no-such-browser-xyz"}

	if _, err := firefoxBinary(""); err == nil || !strings.Contains(err.Error(), "locate firefox") {
		t.Fatalf("error = %v, want a locate-firefox failure", err)
	}
}

func TestFirefoxEnvDisablesSandboxAndDropsRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/stale")

	env := firefoxEnv(false)
	if !containsEnv(env, "MOZ_HEADLESS=1") {
		t.Error("expected MOZ_HEADLESS=1")
	}
	if !containsEnv(env, "MOZ_DISABLE_CONTENT_SANDBOX=1") {
		t.Error("expected the content sandbox disabled when Sandbox is off")
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "XDG_RUNTIME_DIR=") {
			t.Error("XDG_RUNTIME_DIR should be dropped from the child environment")
		}
	}
}

func TestFirefoxEnvKeepsSandboxWhenEnabled(t *testing.T) {
	env := firefoxEnv(true)
	if containsEnv(env, "MOZ_DISABLE_CONTENT_SANDBOX=1") {
		t.Error("the content sandbox must stay on when Sandbox is enabled")
	}
}

func containsEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}

func TestFreeLoopbackPortReturnsUsablePort(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port = %d, want a valid TCP port", port)
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	b := &tailBuffer{max: 4}
	if _, err := b.Write([]byte("abcdef")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if b.String() != "cdef" {
		t.Fatalf("tail = %q, want cdef", b.String())
	}
	if _, err := b.Write([]byte("XY")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if b.String() != "efXY" {
		t.Fatalf("tail = %q, want efXY", b.String())
	}
}

func TestConnectMarionetteGivesUpWhenProcessExits(t *testing.T) {
	port, err := freeLoopbackPort() // now free: nothing is listening on it
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	exited := make(chan struct{})
	close(exited)

	if _, err := connectMarionette(t.Context(), port, exited); err == nil ||
		!strings.Contains(err.Error(), "exited before marionette") {
		t.Fatalf("error = %v, want an exited-before-marionette failure", err)
	}
}

func TestOpenMarionetteSessionCompletesHandshake(t *testing.T) {
	port := fakeMarionetteServer(t, func(req []json.RawMessage) (string, bool) {
		return resultReply(req, `{"value":null}`), true
	})
	exited := make(chan struct{})

	session, err := openMarionetteSession(t.Context(), &exec.Cmd{}, port, time.Second, exited)
	if err != nil {
		t.Fatalf("open marionette session: %v", err)
	}
	if session == nil || session.conn == nil {
		t.Fatal("expected a live session")
	}
	_ = session.conn.close()
}

func TestFirefoxSessionRenderExtractsPage(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		var name string
		_ = json.Unmarshal(req[2], &name)
		switch name {
		case "WebDriver:ExecuteScript":
			var params struct {
				Script string `json:"script"`
			}
			_ = json.Unmarshal(req[3], &params)
			if strings.Contains(params.Script, "contentType") {
				return resultReply(req, `{"value":"text/html"}`), true
			}
			return resultReply(req, `{"value":"<html>hi</html>"}`), true
		case "WebDriver:GetCurrentURL":
			return resultReply(req, `{"value":"http://example.com/final"}`), true
		default:
			return resultReply(req, `{"value":null}`), true
		}
	})
	session := &firefoxSession{conn: client, cmd: &exec.Cmd{}, exited: make(chan struct{})}
	// Consume the greeting the way launch does, so render's commands align.
	if err := session.conn.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	page, err := session.render(context.Background(), "http://example.com/", time.Second)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if page.content != "<html>hi</html>" {
		t.Errorf("content = %q", page.content)
	}
	if page.url != "http://example.com/final" {
		t.Errorf("url = %q", page.url)
	}
	if page.contentType != "text/html" {
		t.Errorf("content type = %q", page.contentType)
	}
}

func TestFirefoxSessionRenderReturnsNavigateError(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		return errorReply(req, `{"error":"unknown error","message":"nav failed"}`), true
	})
	session := &firefoxSession{conn: client, cmd: &exec.Cmd{}, exited: make(chan struct{})}
	if err := session.conn.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	if _, err := session.render(
		context.Background(),
		"http://example.com/",
		time.Second,
	); err == nil || !strings.Contains(err.Error(), "navigate") {
		t.Fatalf("error = %v, want a navigate failure", err)
	}
}

func TestFirefoxSessionAliveTracksProcess(t *testing.T) {
	running := &firefoxSession{exited: make(chan struct{})}
	if !running.alive() {
		t.Error("a running session should report alive")
	}
	exited := make(chan struct{})
	close(exited)
	if (&firefoxSession{exited: exited}).alive() {
		t.Error("an exited session should not report alive")
	}
}

func TestFirefoxSessionCloseRemovesProfile(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		return resultReply(req, `{"value":null}`), true
	})
	profile, err := os.MkdirTemp("", "yago-close-test-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	exited := make(chan struct{})
	close(exited) // the process has already exited, so close returns promptly

	session := &firefoxSession{conn: client, cmd: &exec.Cmd{}, exited: exited, profile: profile}
	session.close()

	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile still present after close: %v", err)
	}
}
