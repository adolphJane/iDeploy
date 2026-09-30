//go:build !windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

var ErrLocked = errors.New("another iDeploy process owns the application runtime")

// Lock is an advisory, process-scoped lock. The lock file is deliberately
// retained after release: removing it could let a third process lock a new
// inode while another process still owns the old one.
type Lock struct {
	file *os.File
}

func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open application lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock application runtime: %w", err)
	}
	if err := file.Truncate(0); err == nil {
		_, _ = file.Seek(0, 0)
		_, _ = file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
		_ = file.Sync()
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock application runtime: %w", unlockErr)
	}
	return closeErr
}
