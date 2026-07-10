package firefoxfetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	errFakeListen      = errors.New("fake listen failure")
	errFakeSetDeadline = errors.New("fake set-deadline failure")
)

// deadlineFailingConn wraps a net.Conn so SetDeadline fails on its failOn-th
// call, letting a test drive openMarionetteSession's set-deadline error paths
// over an otherwise-working scripted pipe.
type deadlineFailingConn struct {
	net.Conn
	calls  int
	failOn int
}

func (c *deadlineFailingConn) SetDeadline(time.Time) error {
	c.calls++
	if c.calls == c.failOn {
		return errFakeSetDeadline
	}
	return nil
}

func newHandshookSession(t *testing.T, client *marionetteConn) *firefoxSession {
	t.Helper()
	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return &firefoxSession{conn: client, cmd: &exec.Cmd{}, exited: make(chan struct{})}
}

func acceptThenClose(t *testing.T) int {
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
		_ = conn.Close()
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

func greetingResponder(req []json.RawMessage) (string, bool) {
	return resultReply(req, `{"value":null}`), true
}

func respondOuterHTMLError(req []json.RawMessage) (string, bool) {
	var name string
	_ = json.Unmarshal(req[2], &name)
	if name == "WebDriver:ExecuteScript" {
		return errorReply(req, `{"error":"javascript error"}`), true
	}
	return resultReply(req, `{"value":null}`), true
}

func respondCurrentURLError(req []json.RawMessage) (string, bool) {
	var name string
	_ = json.Unmarshal(req[2], &name)
	if name == "WebDriver:GetCurrentURL" {
		return errorReply(req, `{"error":"no such window"}`), true
	}
	return resultReply(req, `{"value":"<html></html>"}`), true
}

func respondContentTypeError(req []json.RawMessage) (string, bool) {
	var name string
	_ = json.Unmarshal(req[2], &name)
	if name == "WebDriver:GetCurrentURL" {
		return resultReply(req, `{"value":"http://x/final"}`), true
	}
	var params struct {
		Script string `json:"script"`
	}
	_ = json.Unmarshal(req[3], &params)
	if strings.Contains(params.Script, "contentType") {
		return errorReply(req, `{"error":"javascript error"}`), true
	}
	return resultReply(req, `{"value":"<html></html>"}`), true
}

func fakeSpawn(
	t *testing.T,
	responder func(req []json.RawMessage) (string, bool),
) func(string, []string, []string, int, io.Writer) (*exec.Cmd, <-chan struct{}, error) {
	t.Helper()
	return func(_ string, _, _ []string, port int, _ io.Writer) (*exec.Cmd, <-chan struct{}, error) {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, nil, fmt.Errorf("fake spawn listen: %w", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			serveMarionette(conn, responder)
		}()
		return &exec.Cmd{}, make(chan struct{}), nil
	}
}

func TestFirefoxSessionRenderErrorsWhenDeadlineUnsettable(t *testing.T) {
	client, _ := marionettePair(t)
	_ = client.conn.Close()
	session := &firefoxSession{conn: client, cmd: &exec.Cmd{}, exited: make(chan struct{})}
	if _, err := session.render(
		context.Background(),
		"http://x/",
		time.Second,
	); err == nil || !strings.Contains(err.Error(), "set marionette deadline") {
		t.Fatalf("error = %v, want a set-deadline failure", err)
	}
}

func TestFirefoxSessionRenderPropagatesCommandErrors(t *testing.T) {
	tests := []struct {
		name      string
		responder func(req []json.RawMessage) (string, bool)
		wantErr   string
	}{
		{"outer html fails", respondOuterHTMLError, "read outer html"},
		{"current url fails", respondCurrentURLError, "read final url"},
		{"content type fails", respondContentTypeError, "read content type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, server := marionettePair(t)
			serveMarionette(server, tc.responder)
			session := newHandshookSession(t, client)
			if _, err := session.render(
				context.Background(),
				"http://x/",
				time.Second,
			); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestFirefoxSessionRenderUnblocksOnContextCancel(t *testing.T) {
	client, server := marionettePair(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		var name string
		_ = json.Unmarshal(req[2], &name)
		if name == "WebDriver:Navigate" {
			cancel()
			return "", true
		}
		return resultReply(req, `{"value":null}`), true
	})
	session := newHandshookSession(t, client)
	if _, err := session.render(ctx, "http://x/", time.Second); err == nil ||
		!strings.Contains(err.Error(), "navigate") {
		t.Fatalf("error = %v, want a navigate failure after cancel", err)
	}
}

func TestConnectMarionetteTimesOutWhenUnreachable(t *testing.T) {
	restore := firefoxStartupTimeout
	t.Cleanup(func() { firefoxStartupTimeout = restore })
	firefoxStartupTimeout = 50 * time.Millisecond

	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	exited := make(chan struct{})
	if _, err := connectMarionette(port, exited); err == nil ||
		!strings.Contains(err.Error(), "marionette unreachable") {
		t.Fatalf("error = %v, want an unreachable timeout", err)
	}
}

func TestOpenMarionetteSessionErrorsWhenConnectFails(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	exited := make(chan struct{})
	close(exited)
	if _, err := openMarionetteSession(&exec.Cmd{}, port, time.Second, exited); err == nil ||
		!strings.Contains(err.Error(), "exited before marionette") {
		t.Fatalf("error = %v, want a connect failure", err)
	}
}

func TestOpenMarionetteSessionErrorsOnHandshake(t *testing.T) {
	port := acceptThenClose(t)
	exited := make(chan struct{})
	if _, err := openMarionetteSession(&exec.Cmd{}, port, time.Second, exited); err == nil ||
		!strings.Contains(err.Error(), "read marionette greeting") {
		t.Fatalf("error = %v, want a handshake failure", err)
	}
}

func respondNewSessionError(req []json.RawMessage) (string, bool) {
	return errorReply(req, `{"error":"session error"}`), true
}

func respondSetTimeoutsError(req []json.RawMessage) (string, bool) {
	var name string
	_ = json.Unmarshal(req[2], &name)
	if name == "WebDriver:SetTimeouts" {
		return errorReply(req, `{"error":"bad timeouts"}`), true
	}
	return resultReply(req, `{"value":null}`), true
}

func TestOpenMarionetteSessionErrorsFromCommands(t *testing.T) {
	tests := []struct {
		name      string
		responder func(req []json.RawMessage) (string, bool)
		wantErr   string
	}{
		{"new session fails", respondNewSessionError, "marionette new session"},
		{"set timeouts fails", respondSetTimeoutsError, "marionette set timeouts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			port := fakeMarionetteServer(t, tc.responder)
			exited := make(chan struct{})
			if _, err := openMarionetteSession(
				&exec.Cmd{},
				port,
				time.Second,
				exited,
			); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestFirefoxBinaryFindsBinaryOnPath(t *testing.T) {
	restore := firefoxBinaries
	t.Cleanup(func() { firefoxBinaries = restore })
	firefoxBinaries = []string{"sh"}

	got, err := firefoxBinary("")
	if err != nil {
		t.Fatalf("firefoxBinary: %v", err)
	}
	if !strings.HasSuffix(got, "sh") {
		t.Fatalf("binary = %q, want a resolved sh path", got)
	}
}

func TestKillFirefoxTerminatesRunningProcess(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "/bin/sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	killFirefox(cmd, exited)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("process not reaped after kill")
	}
}

func TestLaunchFirefoxErrorsWhenBinaryMissing(t *testing.T) {
	restore := firefoxBinaries
	t.Cleanup(func() { firefoxBinaries = restore })
	firefoxBinaries = []string{"yago-no-such-browser-zzz"}

	if _, err := launchFirefox(BrowserLaunch{}, ""); err == nil ||
		!strings.Contains(err.Error(), "locate firefox") {
		t.Fatalf("error = %v, want a locate-firefox failure", err)
	}
}

func TestLaunchFirefoxErrorsWhenProfileFails(t *testing.T) {
	if _, err := launchFirefox(
		BrowserLaunch{ExecPath: "/bin/true"},
		"http://no-port-here",
	); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("error = %v, want a profile/proxy failure", err)
	}
}

func TestLaunchFirefoxErrorsWhenSpawnFails(t *testing.T) {
	if _, err := launchFirefox(
		BrowserLaunch{ExecPath: "/nonexistent/yago-firefox-xyz"},
		"",
	); err == nil || !strings.Contains(err.Error(), "start firefox") {
		t.Fatalf("error = %v, want a start-firefox failure", err)
	}
}

func TestLaunchFirefoxErrorsWhenSessionFails(t *testing.T) {
	restore := firefoxStartupTimeout
	t.Cleanup(func() { firefoxStartupTimeout = restore })
	firefoxStartupTimeout = 100 * time.Millisecond

	if _, err := launchFirefox(
		BrowserLaunch{ExecPath: "/bin/true"},
		"",
	); err == nil || !strings.Contains(err.Error(), "firefox stderr") {
		t.Fatalf("error = %v, want a session failure", err)
	}
}

func TestLaunchFirefoxReturnsSessionOnSuccess(t *testing.T) {
	restore := spawnFirefox
	t.Cleanup(func() { spawnFirefox = restore })
	spawnFirefox = fakeSpawn(t, greetingResponder)

	session, err := launchFirefox(
		BrowserLaunch{ExecPath: "/bin/true", Timeout: time.Second},
		"",
	)
	if err != nil {
		t.Fatalf("launchFirefox: %v", err)
	}
	t.Cleanup(func() {
		_ = session.conn.close()
		_ = os.RemoveAll(session.profile)
	})
	if session.profile == "" {
		t.Fatal("expected the profile directory to be recorded on the session")
	}
}

func TestStartFirefoxSessionReturnsError(t *testing.T) {
	restore := firefoxBinaries
	t.Cleanup(func() { firefoxBinaries = restore })
	firefoxBinaries = []string{"yago-no-such-browser-zzz"}

	if _, err := startFirefoxSession(BrowserLaunch{}, ""); err == nil {
		t.Fatal("expected an error when firefox cannot launch")
	}
}

func TestStartFirefoxSessionReturnsSession(t *testing.T) {
	restore := spawnFirefox
	t.Cleanup(func() { spawnFirefox = restore })
	spawnFirefox = fakeSpawn(t, greetingResponder)

	session, err := startFirefoxSession(
		BrowserLaunch{ExecPath: "/bin/true", Timeout: time.Second},
		"",
	)
	if err != nil {
		t.Fatalf("startFirefoxSession: %v", err)
	}
	fs, ok := session.(*firefoxSession)
	if !ok {
		t.Fatalf("session type = %T, want *firefoxSession", session)
	}
	t.Cleanup(func() {
		_ = fs.conn.close()
		_ = os.RemoveAll(fs.profile)
	})
}

func TestFreeLoopbackPortErrorsWhenListenFails(t *testing.T) {
	restore := listenLoopback
	t.Cleanup(func() { listenLoopback = restore })
	listenLoopback = func() (net.Listener, error) { return nil, errFakeListen }

	if _, err := freeLoopbackPort(); err == nil ||
		!strings.Contains(err.Error(), "reserve loopback port") {
		t.Fatalf("error = %v, want a reserve-loopback failure", err)
	}
}

func TestLaunchFirefoxErrorsWhenPortReservationFails(t *testing.T) {
	restore := listenLoopback
	t.Cleanup(func() { listenLoopback = restore })
	listenLoopback = func() (net.Listener, error) { return nil, errFakeListen }

	if _, err := launchFirefox(BrowserLaunch{ExecPath: "/bin/true"}, ""); err == nil ||
		!strings.Contains(err.Error(), "reserve marionette port") {
		t.Fatalf("error = %v, want a reserve-marionette-port failure", err)
	}
}

func TestOpenMarionetteSessionErrorsOnStartupDeadline(t *testing.T) {
	restore := dialMarionette
	t.Cleanup(func() { dialMarionette = restore })
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
	conn := newMarionetteConn(&deadlineFailingConn{Conn: clientSide, failOn: 1})
	dialMarionette = func(int, <-chan struct{}) (*marionetteConn, error) { return conn, nil }

	exited := make(chan struct{})
	if _, err := openMarionetteSession(&exec.Cmd{}, 0, time.Second, exited); err == nil ||
		!strings.Contains(err.Error(), "set marionette deadline") {
		t.Fatalf("error = %v, want a startup set-deadline failure", err)
	}
}

func TestOpenMarionetteSessionErrorsClearingDeadline(t *testing.T) {
	restore := dialMarionette
	t.Cleanup(func() { dialMarionette = restore })
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
	serveMarionette(serverSide, greetingResponder)
	conn := newMarionetteConn(&deadlineFailingConn{Conn: clientSide, failOn: 2})
	dialMarionette = func(int, <-chan struct{}) (*marionetteConn, error) { return conn, nil }

	exited := make(chan struct{})
	if _, err := openMarionetteSession(&exec.Cmd{}, 0, time.Second, exited); err == nil ||
		!strings.Contains(err.Error(), "set marionette deadline") {
		t.Fatalf("error = %v, want a clear-deadline failure", err)
	}
}
