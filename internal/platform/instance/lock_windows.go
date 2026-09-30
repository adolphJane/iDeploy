//go:build windows

package instance

import (
	"errors"
	"fmt"
	"os"
)

var ErrLocked = errors.New("another iDeploy process owns the application runtime")

// Windows is not a release target yet. O_EXCL still prevents concurrent
// owners in normal operation; a future Windows release should replace this
// with LockFileEx so locks are released automatically after a crash.
type Lock struct {
	file *os.File
	path string
}

func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, fmt.Errorf("lock application runtime: %w", err)
	}
	return &Lock{file: file, path: path}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	l.file = nil
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}
