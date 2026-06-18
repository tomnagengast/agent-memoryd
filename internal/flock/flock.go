package flock

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/flock"
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
	fl := flock.New(l.Path)
	timeout := l.LockTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	retry := l.RetryDelay
	if retry == 0 {
		retry = 50 * time.Millisecond
	}
	ok, err := fl.TryLockContext(ctx, retry)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrBusy
		}
		return err
	}
	if !ok {
		return ErrBusy
	}
	defer fl.Unlock()
	return fn()
}

type NoopLocker struct{}

func (NoopLocker) WithLock(_ context.Context, fn func() error) error {
	return fn()
}
