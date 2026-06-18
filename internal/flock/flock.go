package flock

import (
	"context"
	"errors"
	"time"

	goflock "github.com/gofrs/flock"
)

var ErrBusy = errors.New("store busy, try again")

type Locker interface {
	WithLock(ctx context.Context, fn func() error) error
}

type FileLocker struct {
	Path        string
	RetryDelay  time.Duration
	LockTimeout time.Duration
}

func (l *FileLocker) WithLock(ctx context.Context, fn func() error) error {
	release, err := l.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release() //nolint:errcheck
	return fn()
}

// Acquire acquires the advisory file lock and returns a release function.
// The caller must call the returned function when the lock should be released.
// Returns ErrBusy if the lock cannot be acquired within LockTimeout.
func (l *FileLocker) Acquire(ctx context.Context) (release func() error, err error) {
	fl := goflock.New(l.Path)
	timeout := l.LockTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	retry := l.RetryDelay
	if retry == 0 {
		retry = 50 * time.Millisecond
	}
	ok, err := fl.TryLockContext(lockCtx, retry)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrBusy
		}
		return nil, err
	}
	if !ok {
		return nil, ErrBusy
	}
	return func() error {
		return fl.Unlock()
	}, nil
}

type NoopLocker struct{}

func (NoopLocker) WithLock(_ context.Context, fn func() error) error {
	return fn()
}
