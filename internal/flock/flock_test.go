package flock_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/flock"
)

func TestWithLockSerialCallsSucceed(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")
	l := &flock.FileLocker{Path: path, LockTimeout: 2 * time.Second}
	for i := 0; i < 3; i++ {
		err := l.WithLock(context.Background(), func() error { return nil })
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
}

func TestWithLockReturnsErrBusyWhenHeld(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.lock")
	l1 := &flock.FileLocker{Path: path, LockTimeout: 2 * time.Second}
	l2 := &flock.FileLocker{Path: path, LockTimeout: 100 * time.Millisecond}

	var wg sync.WaitGroup
	started := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = l1.WithLock(context.Background(), func() error {
			close(started)
			time.Sleep(500 * time.Millisecond)
			return nil
		})
	}()
	<-started
	err := l2.WithLock(context.Background(), func() error { return nil })
	if !errors.Is(err, flock.ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	wg.Wait()
}
