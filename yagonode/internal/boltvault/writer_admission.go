package boltvault

import (
	"context"
	"fmt"
	"sync"
)

type writerAdmission struct {
	initialize sync.Once
	token      chan struct{}
}

func (a *writerAdmission) acquire(ctx context.Context) error {
	token := a.admissionToken()
	select {
	case <-ctx.Done():
		return fmt.Errorf("context: %w", ctx.Err())
	case <-token:
	}
	if err := ctx.Err(); err != nil {
		a.release()

		return fmt.Errorf("context: %w", err)
	}

	return nil
}

func (a *writerAdmission) acquireUnbounded() {
	<-a.admissionToken()
}

func (a *writerAdmission) release() {
	a.admissionToken() <- struct{}{}
}

func (a *writerAdmission) admissionToken() chan struct{} {
	a.initialize.Do(func() {
		a.token = make(chan struct{}, 1)
		a.token <- struct{}{}
	})

	return a.token
}
