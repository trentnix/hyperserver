package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"backup"}, {"unknown", "archive"}, {"backup", "archive", "extra"}, {"verify", "archive", "extra"}, {"restore", "archive", "target"}, {"adopt-schema"}, {"upgrade-schema"}, {"downgrade-schema"}} {
		if err := run(t.Context(), args); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("args %q: got %v, want usage error", args, err)
		}
	}
}

func TestVerifyDoesNotNeedLiveConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	err := run(t.Context(), []string{"verify", "missing.zip"})
	var pathError *os.PathError
	if !errors.As(err, &pathError) || filepath.Base(pathError.Path) != "missing.zip" || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verify did not reach archive without configuration: %v", err)
	}
}
