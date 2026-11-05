package maintenance

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	directory := t.TempDir()
	first, err := Lock(directory)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(directory); err == nil {
		second.Close()
		t.Fatal("overlapping lock accepted")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Lock(directory)
	if err != nil {
		t.Fatal("released lock cannot be reacquired", err)
	}
	next.Close()
}

func TestLockDirectoryScope(t *testing.T) {
	directory := t.TempDir()
	first, err := Lock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	alias := filepath.Join(t.TempDir(), "application")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(alias); err == nil {
		second.Close()
		t.Fatal("directory alias bypassed the lock")
	}
	other, err := Lock(t.TempDir())
	if err != nil {
		t.Fatal("independent application directory was blocked", err)
	}
	other.Close()
	before, err := first.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(directory, ".blog-maintenance.lock"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("closing a lock must preserve its file: %v", err)
	}
}

func TestLockProcessExit(t *testing.T) {
	const helperDirectory = "BLOG_TEST_LOCK_DIRECTORY"
	if directory := os.Getenv(helperDirectory); directory != "" {
		lock, err := Lock(directory)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Println("locked")
		// The parent kills this process, bypassing deferred cleanup.
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}

	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockProcessExit$")
	cmd.Env = append(os.Environ(), helperDirectory+"="+directory)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if ready, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || ready != "locked\n" {
		t.Fatalf("child did not acquire lock: %q, %v", ready, err)
	}
	if lock, err := Lock(directory); err == nil {
		lock.Close()
		t.Fatal("lock held by another process was acquired")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed process exited successfully")
	}
	lock, err := Lock(directory)
	if err != nil {
		t.Fatal("process exit did not release lock", err)
	}
	lock.Close()
}
