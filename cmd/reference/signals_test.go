package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

func TestShutdownSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("subprocess interrupt delivery is not supported on Windows")
	}
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/force=%t", signal, force), func(t *testing.T) {
				testShutdownSignal(t, signal, force)
			})
		}
	}
}

func testShutdownSignal(t *testing.T, signal syscall.Signal, force bool) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	yaml := strings.Replace(startupTestConfig, "port: 8080", "port: 0", 1)
	yaml += fmt.Sprintf("app:\n  workingDirectory: %q\n", root)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStartupHelperProcess$")
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HYPERSERVER_") && !strings.HasPrefix(entry, "HS_STARTUP_TEST_HELPER=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HS_STARTUP_TEST_HELPER=signals")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var processErr error
	exited := make(chan struct{})
	go func() {
		processErr = cmd.Wait()
		close(exited)
	}()
	defer func() {
		cancel()
		<-exited
	}()
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	awaitLine := func(prefix string) string {
		t.Helper()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("process output ended before %q", prefix)
				}
				if value, ok := strings.CutPrefix(line, prefix); ok {
					return value
				}
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %q", prefix)
			}
		}
	}

	address := awaitLine("Starting server at ")
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		res, err := client.Post("http://"+address+"/signal-test", "text/plain", nil)
		if err == nil {
			res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("active request returned %d", res.StatusCode)
			}
		}
		response <- err
	}()
	awaitLine("Signal test request started")
	if err := cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	// This message follows signal unregistration, so the second signal cannot
	// race with restoring its default behavior.
	awaitLine("Shutting down. Send another interrupt to force exit.")
	select {
	case <-exited:
		t.Fatalf("first signal exited before the request finished: %v", processErr)
	default:
	}

	if force {
		if err := cmd.Process.Signal(signal); err != nil {
			t.Fatal(err)
		}
	} else {
		// Release the active request without opening another HTTP connection.
		if _, err := stdin.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		awaitResult(t, response)
	}
	select {
	case <-exited:
	case <-ctx.Done():
		t.Fatal("process did not exit after shutdown")
	}
	if force {
		var exitErr *exec.ExitError
		if !errors.As(processErr, &exitErr) {
			t.Fatalf("forced shutdown error = %v, want signal termination", processErr)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != signal {
			t.Fatalf("process did not terminate from %s: %v", signal, processErr)
		}
	} else if processErr != nil {
		t.Fatal("graceful shutdown failed:", processErr)
	}
}

func runSignalTestHelper(t *testing.T) {
	t.Helper()
	release := make(chan struct{})
	go func() {
		var command [1]byte
		io.ReadFull(os.Stdin, command[:])
		close(release)
	}()
	handlers.Register(handlers.Descriptor{Name: "signal-test", New: func() handlers.Handler { return &signalTestModule{release: release} }})
	main()
}

type signalTestModule struct {
	release <-chan struct{}
}

func (*signalTestModule) Init(context.Context, *server.ApplicationServer) error { return nil }

func (m *signalTestModule) Routes(mux *routing.Routes) error {
	mux.HandleFunc("POST /signal-test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Signal test request started")
		<-m.release
		w.WriteHeader(http.StatusNoContent)
	})
	return nil
}
