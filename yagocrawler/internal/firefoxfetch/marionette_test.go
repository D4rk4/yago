package firefoxfetch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

const marionetteGreeting = `{"applicationType":"gecko","marionetteProtocol":3}`

// marionettePair returns a marionetteConn wired over loopback TCP to a raw
// server connection the test scripts.
func marionettePair(t *testing.T) (*marionetteConn, net.Conn) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	type accepted struct {
		conn net.Conn
		err  error
	}
	accepts := make(chan accepted, 1)
	go func() {
		conn, err := listener.Accept()
		accepts <- accepted{conn, err}
	}()

	client, err := (&net.Dialer{}).DialContext(
		context.Background(),
		"tcp",
		listener.Addr().String(),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	got := <-accepts
	if got.err != nil {
		t.Fatalf("accept: %v", got.err)
	}
	t.Cleanup(func() { _ = got.conn.Close() })

	return newMarionetteConn(client), got.conn
}

// serveMarionette runs a scripted Marionette server on conn: it sends the
// greeting, then replies to each command via responder until responder asks it
// to stop or the client closes.
func serveMarionette(
	conn net.Conn,
	responder func(request []json.RawMessage) (reply string, keepGoing bool),
) {
	go func() {
		reader := bufio.NewReader(conn)
		if err := rawWriteFrame(conn, marionetteGreeting); err != nil {
			return
		}
		for {
			frame, err := rawReadFrame(reader)
			if err != nil {
				return
			}
			var request []json.RawMessage
			if err := json.Unmarshal([]byte(frame), &request); err != nil {
				return
			}
			reply, keepGoing := responder(request)
			if reply != "" {
				if err := rawWriteFrame(conn, reply); err != nil {
					return
				}
			}
			if !keepGoing {
				return
			}
		}
	}()
}

func rawReadFrame(reader *bufio.Reader) (string, error) {
	length := 0
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return "", fmt.Errorf("read frame length: %w", err)
		}
		if b == ':' {
			break
		}
		length = length*10 + int(b-'0')
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", fmt.Errorf("read frame payload: %w", err)
	}
	return string(payload), nil
}

func rawWriteFrame(w io.Writer, payload string) error {
	if _, err := io.WriteString(w, strconv.Itoa(len(payload))+":"+payload); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

func resultReply(request []json.RawMessage, result string) string {
	return "[1," + string(request[1]) + ",null," + result + "]"
}

func errorReply(request []json.RawMessage, errObject string) string {
	return "[1," + string(request[1]) + "," + errObject + ",null]"
}

func TestMarionetteHandshakeThenExecuteScript(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		return resultReply(req, `{"value":"<html><body>ok</body></html>"}`), false
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	html, err := client.executeScriptString(jsDocumentOuterHTML)
	if err != nil {
		t.Fatalf("execute script: %v", err)
	}
	if html != "<html><body>ok</body></html>" {
		t.Fatalf("html = %q", html)
	}
}

func TestMarionetteCommandSurfacesRemoteError(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		return errorReply(req, `{"error":"timeout","message":"page load timeout"}`), false
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	err := client.navigate("http://slow.example/")
	if err == nil || !strings.Contains(err.Error(), "page load timeout") {
		t.Fatalf("navigate error = %v, want page load timeout", err)
	}
}

func TestMarionetteRejectsMismatchedReplyID(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(_ []json.RawMessage) (string, bool) {
		return `[1,999,null,{"value":null}]`, false
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_, err := client.currentURL()
	if err == nil || !strings.Contains(err.Error(), "desynchronized") {
		t.Fatalf("error = %v, want desynchronized stream", err)
	}
}

func TestMarionetteCurrentURLDecodesValue(t *testing.T) {
	client, server := marionettePair(t)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		return resultReply(req, `{"value":"https://final.example/page"}`), false
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	got, err := client.currentURL()
	if err != nil {
		t.Fatalf("current url: %v", err)
	}
	if got != "https://final.example/page" {
		t.Fatalf("url = %q", got)
	}
}

func TestMarionetteSendsExpectedCommandSequence(t *testing.T) {
	client, server := marionettePair(t)
	names := make(chan string, 4)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		var name string
		_ = json.Unmarshal(req[2], &name)
		names <- name
		return resultReply(req, `{"value":null}`), true
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := client.newSession(); err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := client.setPageLoadTimeout(15 * time.Second); err != nil {
		t.Fatalf("set timeouts: %v", err)
	}

	if got := <-names; got != "WebDriver:NewSession" {
		t.Fatalf("first command = %q", got)
	}
	if got := <-names; got != "WebDriver:SetTimeouts" {
		t.Fatalf("second command = %q", got)
	}
}

func TestMarionetteSetPageLoadTimeoutSkipsWhenZero(t *testing.T) {
	client, server := marionettePair(t)
	served := make(chan struct{}, 1)
	serveMarionette(server, func(req []json.RawMessage) (string, bool) {
		served <- struct{}{}
		return resultReply(req, `{"value":null}`), true
	})

	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := client.setPageLoadTimeout(0); err != nil {
		t.Fatalf("set timeouts: %v", err)
	}
	select {
	case <-served:
		t.Fatal("a zero timeout should send no SetTimeouts command")
	case <-time.After(100 * time.Millisecond):
	}
}
