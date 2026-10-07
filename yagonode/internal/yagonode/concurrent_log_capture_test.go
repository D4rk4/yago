package yagonode

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type concurrentLogCapture struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (capture *concurrentLogCapture) Write(value []byte) (int, error) {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	written, _ := capture.buffer.Write(value)

	return written, nil
}

func (capture *concurrentLogCapture) String() string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return capture.buffer.String()
}

func (capture *concurrentLogCapture) Bytes() []byte {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return bytes.Clone(capture.buffer.Bytes())
}

func (capture *concurrentLogCapture) Reset() {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	capture.buffer.Reset()
}

func (capture *concurrentLogCapture) Len() int {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return capture.buffer.Len()
}

func TestConcurrentLogCaptureAllowsConcurrentWritesAndSnapshots(t *testing.T) {
	var capture concurrentLogCapture
	logger := slog.New(slog.NewJSONHandler(&capture, nil))
	start := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		<-start
		for range 128 {
			logger.InfoContext(
				context.Background(),
				"capture event",
				slog.String("payload", strings.Repeat("x", 4096)),
			)
		}
		close(finished)
	}()
	close(start)
	for {
		_ = capture.String()
		select {
		case <-finished:
			if got := strings.Count(capture.String(), `"msg":"capture event"`); got != 128 {
				t.Fatalf("captured events = %d, want 128", got)
			}
			return
		default:
		}
	}
}
