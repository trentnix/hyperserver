//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package maintenance

import (
	"errors"
	"os"
)

// Lock fails explicitly on platforms without the supported process-lock implementation.
func Lock(string) (*os.File, error) {
	return nil, errors.New("blog maintenance locking requires Linux, macOS, or BSD")
}
