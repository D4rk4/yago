package firefoxfetch

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// firefoxStartupTimeout bounds how long launchFirefox waits for a freshly
// spawned Firefox to accept a Marionette connection. A cold headless start on a
// loaded box (including the QEMU prod VM) can take several seconds.
var firefoxStartupTimeout = 45 * time.Second

// firefoxBinaries names the binaries firefoxBinary probes on PATH when the
// operator leaves the browser path empty, ESR first to match the Debian package
// dependency.
var firefoxBinaries = []string{"firefox-esr", "firefox"}

var createFirefoxProfile = writeFirefoxProfile

// firefoxSession is one long-lived headless Firefox process and its Marionette
// session. The manager keeps a single session for the crawler's lifetime and
// drives every page through it; a broken session is closed and replaced.
type firefoxSession struct {
	cmd     *exec.Cmd
	conn    *marionetteConn
	profile string
	exited  <-chan struct{}
}

// launchFirefox starts a headless Firefox, connects Marionette, opens a
// WebDriver session, and arms the page-load timeout. Every failure path kills
// the process and removes the throwaway profile so a failed launch leaks
// neither a process nor a temp directory.
func launchFirefox(
	ctx context.Context,
	launch BrowserLaunch,
	proxyURL string,
) (*firefoxSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("launch firefox: %w", err)
	}
	binary, err := firefoxBinary(launch.ExecPath)
	if err != nil {
		return nil, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, fmt.Errorf("reserve marionette port: %w", err)
	}
	profile, err := createFirefoxProfile(firefoxProfile{
		MarionettePort: port,
		ProxyURL:       proxyURL,
		UserAgent:      launch.UserAgent,
		Sandbox:        launch.Sandbox,
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = os.RemoveAll(profile)

		return nil, fmt.Errorf("launch firefox: %w", err)
	}

	args := []string{
		"--headless", "--no-remote", "--new-instance",
		"-profile", profile, "--marionette",
	}
	stderr := &tailBuffer{max: 4096}
	cmd, exited, err := spawnFirefox(binary, args, firefoxEnv(launch.Sandbox), port, stderr)
	if err != nil {
		_ = os.RemoveAll(profile)
		return nil, fmt.Errorf("start firefox %s: %w", binary, err)
	}

	session, err := openMarionetteSession(ctx, cmd, port, launch.Timeout, exited)
	if err != nil {
		killFirefox(cmd, exited)
		_ = os.RemoveAll(profile)
		return nil, fmt.Errorf("%w; firefox stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	session.profile = profile
	return session, nil
}

var spawnFirefox = func(
	binary string,
	args, env []string,
	_ int,
	stderr io.Writer,
) (*exec.Cmd, <-chan struct{}, error) {
	//nolint:gosec // launches the operator-configured browser binary.
	cmd := exec.CommandContext(context.Background(), binary, args...) // nosemgrep
	cmd.Env = env
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err //nolint:wrapcheck // launchFirefox wraps this as "start firefox %s".
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	return cmd, exited, nil
}

var dialMarionette = connectMarionette

// openMarionetteSession dials Marionette (retrying while Firefox warms up),
// reads the greeting, and starts a WebDriver session under the startup
// deadline. On success the deadline is cleared and per-fetch deadlines take
// over.
func openMarionetteSession(
	ctx context.Context,
	cmd *exec.Cmd,
	port int,
	pageLoad time.Duration,
	exited <-chan struct{},
) (*firefoxSession, error) {
	conn, err := dialMarionette(ctx, port, exited)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(firefoxStartupTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.setDeadline(deadline); err != nil {
		_ = conn.close()
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.setDeadline(time.Now()) })
	defer stop()
	if err := conn.handshake(); err != nil {
		_ = conn.close()
		return nil, marionetteSessionError(ctx, "marionette handshake", err)
	}
	if err := conn.newSession(); err != nil {
		_ = conn.close()
		return nil, marionetteSessionError(ctx, "marionette new session", err)
	}
	if err := conn.setPageLoadTimeout(pageLoad); err != nil {
		_ = conn.close()
		return nil, marionetteSessionError(ctx, "marionette set timeouts", err)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.close()

		return nil, fmt.Errorf("open marionette session: %w", err)
	}
	if err := conn.setDeadline(time.Time{}); err != nil {
		_ = conn.close()
		return nil, err
	}
	return &firefoxSession{cmd: cmd, conn: conn, exited: exited}, nil
}

func marionetteSessionError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}

	return fmt.Errorf("%s: %w", operation, err)
}

// connectMarionette dials the Marionette port, retrying until Firefox is
// listening or the startup budget runs out, and gives up early if the process
// exits.
func connectMarionette(
	ctx context.Context,
	port int,
	exited <-chan struct{},
) (*marionetteConn, error) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	dialer := net.Dialer{Timeout: time.Second}
	deadline := time.Now().Add(firefoxStartupTimeout)
	var lastErr error
	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			return newMarionetteConn(conn), nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("connect marionette: %w", ctx.Err())
		}
		lastErr = err
		select {
		case <-exited:
			return nil, fmt.Errorf("firefox exited before marionette listened on %s", addr)
		case <-ctx.Done():
			return nil, fmt.Errorf("connect marionette: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf(
				"marionette unreachable on %s within %s: %w",
				addr,
				firefoxStartupTimeout,
				lastErr,
			)
		}
	}
}

// render drives one page through the session: navigate, read the rendered DOM,
// the final URL after redirects, and the content type. The deadline (from the
// caller's context or the fallback timeout) bounds the whole sequence, and a
// context cancellation unblocks a stuck socket read.
func (s *firefoxSession) render(
	ctx context.Context,
	rawURL string,
	timeout time.Duration,
) (renderedPage, error) {
	deadline, ok := ctx.Deadline()
	if !ok && timeout > 0 {
		deadline, ok = time.Now().Add(timeout), true
	}
	if ok {
		if err := s.conn.setDeadline(deadline); err != nil {
			return renderedPage{}, err
		}
		defer func() { _ = s.conn.setDeadline(time.Time{}) }()
	}
	stop := context.AfterFunc(ctx, func() { _ = s.conn.setDeadline(time.Now()) })
	defer stop()

	if err := s.conn.navigate(rawURL); err != nil {
		return renderedPage{}, fmt.Errorf("navigate %s: %w", rawURL, err)
	}
	content, err := s.conn.executeScriptString(jsDocumentOuterHTML)
	if err != nil {
		return renderedPage{}, fmt.Errorf("read outer html %s: %w", rawURL, err)
	}
	finalURL, err := s.conn.currentURL()
	if err != nil {
		return renderedPage{}, fmt.Errorf("read final url %s: %w", rawURL, err)
	}
	contentType, err := s.conn.executeScriptString(jsDocumentContentType)
	if err != nil {
		return renderedPage{}, fmt.Errorf("read content type %s: %w", rawURL, err)
	}
	return renderedPage{url: finalURL, content: content, contentType: contentType}, nil
}

// alive reports whether the Firefox process is still running.
func (s *firefoxSession) alive() bool {
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// close ends the Marionette session, kills the process, and removes the
// throwaway profile.
func (s *firefoxSession) close() {
	if s.conn != nil {
		_ = s.conn.setDeadline(time.Now().Add(2 * time.Second))
		_ = s.conn.quit()
		_ = s.conn.close()
	}
	killFirefox(s.cmd, s.exited)
	if s.profile != "" {
		_ = os.RemoveAll(s.profile)
	}
}

func killFirefox(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
	}
}

func firefoxBinary(execPath string) (string, error) {
	if execPath != "" && !LooksLikeChromium(execPath) {
		return execPath, nil
	}
	for _, name := range firefoxBinaries {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("locate firefox: none of %v found on PATH", firefoxBinaries)
}

// LooksLikeChromium reports whether execPath names a Chromium- or Chrome-family
// binary rather than Firefox. The crawler's slow path drives the browser over
// Marionette, which only Firefox speaks, so a Chromium path is a
// misconfiguration — most often a browser path left set from before the crawler
// moved to Firefox — and firefoxBinary discards it in favor of Firefox on PATH.
func LooksLikeChromium(execPath string) bool {
	base := strings.ToLower(filepath.Base(execPath))

	return strings.Contains(base, "chromium") || strings.Contains(base, "chrome")
}

// firefoxEnv derives the child environment: it forces headless mode, drops
// XDG_RUNTIME_DIR (a stale value trips the "already running" remote check on a
// fresh profile), and disables the content sandbox when the operator has not
// opted in, matching the security.sandbox.content.level pref for hosts that
// restrict unprivileged user namespaces.
func firefoxEnv(sandbox bool) []string {
	parent := os.Environ()
	env := make([]string, 0, len(parent)+2)
	for _, kv := range parent {
		if strings.HasPrefix(kv, "XDG_RUNTIME_DIR=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "MOZ_HEADLESS=1")
	if !sandbox {
		env = append(env, "MOZ_DISABLE_CONTENT_SANDBOX=1")
	}
	return env
}

var listenLoopback = func() (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
}

func freeLoopbackPort() (int, error) {
	listener, err := listenLoopback()
	if err != nil {
		return 0, fmt.Errorf("reserve loopback port: %w", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// tailBuffer keeps only the last max bytes written to it, so a launch failure
// can surface Firefox's most recent stderr without unbounded buffering.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
