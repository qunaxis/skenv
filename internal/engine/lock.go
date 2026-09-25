package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lock takes an exclusive, non-blocking flock on ~/.local/state/skenv/lock
// so that autostart and a manual run never interleave their state writes.
func (e *Engine) lock() error {
	dir := e.layout.State()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("another skenv process is running (lock %s); try again when it finishes", e.show(f.Name()))
		}
		return err
	}
	e.lockFile = f
	return nil
}

// Close releases the lock taken by Open.
func (e *Engine) Close() {
	if e.lockFile != nil {
		_ = syscall.Flock(int(e.lockFile.Fd()), syscall.LOCK_UN)
		_ = e.lockFile.Close()
		e.lockFile = nil
	}
}
