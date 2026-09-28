package daemoncmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// dataLockFile is held locked by the daemon that uses its data directory.
const dataLockFile = "matrixclawd.lock"

// lockDataDir takes the data directory for this daemon until release; it fails
// while another daemon holds it.
func lockDataDir(dir string) (release func() error, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, dataLockFile)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another matrixclawd already runs on %s (it holds %s)", dir, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return file.Close, nil
}
