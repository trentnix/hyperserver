//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Package maintenance coordinates the blog server and offline maintenance.
package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock excludes other cooperating processes until the returned file is closed.
// The persistent lock file must not be removed: doing so would split the lock.
// Kernel locks are released automatically if a process exits unexpectedly.
func Lock(directory string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(directory, ".blog-maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("blog or maintenance is already running in %s: %w", directory, err)
	}
	return file, nil
}
