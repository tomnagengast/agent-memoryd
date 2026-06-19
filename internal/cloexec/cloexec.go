package cloexec

import (
	"errors"
	"os"
	"strconv"
	"syscall"
)

// MarkOpenFiles marks currently open non-stdio file descriptors close-on-exec.
// Native libraries may open descriptors without FD_CLOEXEC; daemon subprocesses
// must not inherit the zvec collection lock.
func MarkOpenFiles() error {
	if err := markOpenFiles("/proc/self/fd"); err == nil {
		return nil
	} else if !isMissing(err) {
		return err
	}
	if err := markOpenFiles("/dev/fd"); err == nil {
		return nil
	} else if !isMissing(err) {
		return err
	}
	return nil
}

func markOpenFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd <= 2 {
			continue
		}
		syscall.CloseOnExec(fd)
	}
	return nil
}

func isMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
