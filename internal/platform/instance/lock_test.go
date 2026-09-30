//go:build !windows

package instance

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLockPreventsConcurrentOwnerAndCanBeReacquired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ideploy.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	if _, err := Acquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("reacquire lock: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}
