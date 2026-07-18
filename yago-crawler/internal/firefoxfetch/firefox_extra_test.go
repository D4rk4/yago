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
	"sync"
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

type readSignalingConn struct {
	net.Conn
	once    sync.Once
	started chan struct{}
}

type cancelOnSecondReplyConn struct {
	net.Conn
	cancel context.CancelFunc
	once   sync.Once
	seen   string
}

func (c *cancelOnSecondReplyConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.seen += string(p[:n])
	if strings.Contains(c.seen, "[1,2,") {
		c.once.Do(c.cancel)
	}
	if err != nil {
		return n, fmt.Errorf("read cancellation connection: %w", err)
	}

	return n, nil
}

func (c *readSignalingConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	n, err := c.Conn.Read(p)
	if err != nil {
		return n, fmt.Errorf("read signaling connection: %w", err)
	}

	return n, nil
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
	if _, err := connectMarionette(t.Context(), port, exited); err == nil ||
		!strings.Contains(err.Error(), "marionette unreachable") {
		t.Fatalf("error = %v, want an unreachable timeout", err)
	}
}

func TestConnectMarionetteHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connectMarionette(ctx, 1, make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("connect error = %v, want cancellation", err)
	}
}

func TestConnectMarionetteHonorsContextDuringRetry(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := connectMarionette(
		ctx,
		port,
		make(chan struct{}),
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("connect error = %v, want deadline", err)
	}
}

func TestOpenMarionetteSessionErrorsWhenConnectFails(t *testing.T) {
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatalf("freeLoopbackPort: %v", err)
	}
	exited := make(chan struct{})
	close(exited)
	if _, err := openMarionetteSession(
		t.Context(),
		&exec.Cmd{},
		port,
		time.Second,
		exited,
	); err == nil ||
		!strings.Contains(err.Error(), "exited before marionette") {
		t.Fatalf("error = %v, want a connect failure", err)
	}
}

func TestOpenMarionetteSessionErrorsOnHandshake(t *testing.T) {
	port := acceptThenClose(t)
	exited := make(chan struct{})
	if _, err := openMarionetteSession(
		t.Context(),
		&exec.Cmd{},
		port,
		time.Second,
		exited,
	); err == nil ||
		!strings.Contains(err.Error(), "read marionette greeting") {
		t.Fatalf("error = %v, want a handshake failure", err)
	}
}

func TestOpenMarionetteSessionUsesEarlierContextDeadline(t *testing.T) {
	port := fakeMarionetteServer(t, greetingResponder)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	session, err := openMarionetteSession(ctx, &exec.Cmd{}, port, time.Second, make(chan struct{}))
	if err != nil {
		t.Fatalf("open marionette session: %v", err)
	}
	_ = session.conn.close()
}

func TestOpenMarionetteSessionTranslatesHandshakeCancellation(t *testing.T) {
	restore := dialMarionette
	t.Cleanup(func() { dialMarionette = restore })
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
	started := make(chan struct{})
	conn := newMarionetteConn(&readSignalingConn{Conn: clientSide, started: started})
	dialMarionette = func(context.Context, int, <-chan struct{}) (*marionetteConn, error) {
		return conn, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := openMarionetteSession(ctx, &exec.Cmd{}, 0, time.Second, make(chan struct{}))
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) ||
		!strings.Contains(err.Error(), "marionette handshake") {
		t.Fatalf("open error = %v, want handshake cancellation", err)
	}
}

func TestOpenMarionetteSessionRejectsCancellationAfterTimeoutSetup(t *testing.T) {
	restore := dialMarionette
	t.Cleanup(func() { dialMarionette = restore })
	clientSide, serverSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); _ = serverSide.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dialMarionette = func(context.Context, int, <-chan struct{}) (*marionetteConn, error) {
		return newMarionetteConn(&cancelOnSecondReplyConn{Conn: clientSide, cancel: cancel}), nil
	}
	serveMarionette(serverSide, greetingResponder)
	_, err := openMarionetteSession(ctx, &exec.Cmd{}, 0, time.Second, make(chan struct{}))
	if !errors.Is(err, context.Canceled) ||
		!strings.Contains(err.Error(), "open marionette session") {
		t.Fatalf("open error = %v, want post-timeout cancellation", err)
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
				t.Context(),
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

	if _, err := launchFirefox(t.Context(), BrowserLaunch{}, ""); err == nil ||
		!strings.Contains(err.Error(), "locate firefox") {
		t.Fatalf("error = %v, want a locate-firefox failure", err)
	}
}

func TestLaunchFirefoxRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := launchFirefox(
		ctx,
		BrowserLaunch{ExecPath: "/bin/true"},
		"",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("launch error = %v, want cancellation", err)
	}
}

func TestLaunchFirefoxRemovesProfileWhenCanceledAfterCreation(t *testing.T) {
	restore := createFirefoxProfile
	t.Cleanup(func() { createFirefoxProfile = restore })
	ctx, cancel := context.WithCancel(t.Context())
	var profile string
	createFirefoxProfile = func(configuration firefoxProfile) (string, error) {
		created, err := writeFirefoxProfile(configuration)
		profile = created
		cancel()

		return created, err
	}
	if _, err := launchFirefox(
		ctx,
		BrowserLaunch{ExecPath: "/bin/true"},
		"",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("launch error = %v, want cancellation", err)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("profile remains after cancellation: %v", err)
	}
}

func TestLaunchFirefoxErrorsWhenProfileFails(t *testing.T) {
	if _, err := launchFirefox(
		t.Context(),
		BrowserLaunch{ExecPath: "/bin/true"},
		"http://no-port-here",
	); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("error = %v, want a profile/proxy failure", err)
	}
}

func TestLaunchFirefoxErrorsWhenSpawnFails(t *testing.T) {
	if _, err := launchFirefox(
		t.Context(),
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
		t.Context(),
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
		t.Context(),
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

	if _, err := startFirefoxSession(t.Context(), BrowserLaunch{}, ""); err == nil {
		t.Fatal("expected an error when firefox cannot launch")
	}
}

func TestStartFirefoxSessionReturnsSession(t *testing.T) {
	restore := spawnFirefox
	t.Cleanup(func() { spawnFirefox = restore })
	spawnFirefox = fakeSpawn(t, greetingResponder)

	session, err := startFirefoxSession(
		t.Context(),
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

	if _, err := launchFirefox(t.Context(), BrowserLaunch{ExecPath: "/bin/true"}, ""); err == nil ||
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
	dialMarionette = func(context.Context, int, <-chan struct{}) (*marionetteConn, error) { return conn, nil }

	exited := make(chan struct{})
	if _, err := openMarionetteSession(
		t.Context(),
		&exec.Cmd{},
		0,
		time.Second,
		exited,
	); err == nil ||
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
	dialMarionette = func(context.Context, int, <-chan struct{}) (*marionetteConn, error) { return conn, nil }

	exited := make(chan struct{})
	if _, err := openMarionetteSession(
		t.Context(),
		&exec.Cmd{},
		0,
		time.Second,
		exited,
	); err == nil ||
		!strings.Contains(err.Error(), "set marionette deadline") {
		t.Fatalf("error = %v, want a clear-deadline failure", err)
	}
}
