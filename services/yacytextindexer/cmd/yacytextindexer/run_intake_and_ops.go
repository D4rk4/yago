package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacytextindexer/internal/pageintake"
)

const (
	opsReadHeaderLimit = 10 * time.Second
	opsShutdownLimit   = 15 * time.Second
)

func runIntakeAndOps(
	ctx context.Context,
	intake *pageintake.CrawledPageConsumer,
	opsServer *http.Server,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	intakeDone := make(chan struct{})
	var intakeErr error
	go func() {
		intakeErr = intake.Run(runCtx)
		close(intakeDone)
	}()

	opsErr := make(chan error, 1)
	go func() {
		if err := opsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			opsErr <- err
			return
		}
		opsErr <- nil
	}()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-opsErr:
	case <-intakeDone:
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), opsShutdownLimit)
	defer shutdownCancel()
	_ = opsServer.Shutdown(shutdownCtx)
	<-intakeDone
	if serveErr != nil {
		return serveErr
	}
	if intakeErr != nil {
		return fmt.Errorf("run crawled page consumer: %w", intakeErr)
	}
	return nil
}
