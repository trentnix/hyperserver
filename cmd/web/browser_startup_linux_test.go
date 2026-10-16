package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBrowserStartup is an opt-in probe, not an application browser test.
// Screenshot mode lets Firefox exit naturally and export its startup profile.
func TestBrowserStartup(t *testing.T) {
	mode := os.Getenv("HS_TEST_BROWSER_STARTUP")
	if mode == "" {
		t.Skip("set HS_TEST_BROWSER_STARTUP to baseline, profile, or trace")
	}
	if mode != "baseline" && mode != "profile" && mode != "trace" {
		t.Fatal("HS_TEST_BROWSER_STARTUP must be baseline, profile, or trace")
	}
	browser := os.Getenv("HS_TEST_FIREFOX")
	if browser == "" {
		t.Fatal("HS_TEST_FIREFOX must name a Firefox executable")
	}

	// A failure must not prevent the later-launch comparison from running.
	for _, name := range []string{"first", "later-1", "later-2"} {
		t.Run(name, func(t *testing.T) { runBrowserStartup(t, mode, browser) })
	}
}

func runBrowserStartup(t *testing.T, mode, browser string) {
	t.Helper()
	ready := make(chan time.Time, 1)
	logs := &capturedLogs{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><title>Startup probe</title><h1>Startup probe</h1><script>fetch('/ready', {method: 'POST'});</script>`)
	})
	mux.HandleFunc("POST /ready", func(w http.ResponseWriter, r *http.Request) {
		select {
		case ready <- time.Now():
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(browserDiagnosticHandler(mux, make(chan string, 1), logs))
	defer server.Close()

	artifacts, profile := browserArtifactDirectory(t), t.TempDir()
	logPath := filepath.Join(artifacts, "firefox.log")
	output, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()

	// The extra observation time preserves evidence. Readiness after 30 seconds
	// still fails, even if the browser eventually completes and saves a profile.
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd, err := browserStartupCommand(ctx, mode, browser, profile, artifacts, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = output, output
	writeBrowserArtifact(t, artifacts, "launch.txt", fmt.Sprintf("Test: %s\nMode: %s\nArguments: %q\nReadiness limit: 30s\nObservation limit: 90s\n", t.Name(), mode, cmd.Args))
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	exitErr := observeBrowserStartup(exited, time.Until(started.Add(30*time.Second)), func() {
		// In trace mode the launcher is strace. The process tree and trace identify
		// Firefox and its children. Do not label the launcher's counters as Firefox's.
		waits := "Launcher is strace. See syscalls.log for browser activity.\n"
		if mode != "trace" {
			waits = browserWaitSnapshot(cmd.Process.Pid)
		}
		writeBrowserArtifact(t, artifacts, "at-limit.txt", fmt.Sprintf("Captured at %s\nLauncher PID: %d\n%s\n%s\nIndependent server probe: %s\n%s",
			time.Now().Format(time.RFC3339Nano), cmd.Process.Pid, waits, browserProcessSnapshot(), probeBrowserServer(server.URL), browserDiagnostics(profile, logPath, logs.String())))
	})

	var readyAt time.Time
	select {
	case readyAt = <-ready:
	default:
	}
	var readyElapsed time.Duration
	if !readyAt.IsZero() {
		readyElapsed = readyAt.Sub(started)
	}
	result := browserStartupResult(started, readyAt, 30*time.Second, exitErr, ctx.Err())
	writeBrowserArtifact(t, artifacts, "timing.txt", fmt.Sprintf("Mode: %s\nStarted at: %s\nReady at: %s\nReadiness elapsed: %s (valid only when readiness was observed)\nTotal elapsed: %s\nResult: %v\nHTTP requests:\n%s",
		mode, started.Format(time.RFC3339Nano), readyAt.Format(time.RFC3339Nano), readyElapsed, time.Since(started), result, logs.String()))
	if result != nil {
		t.Error(result)
	}
	if mode == "profile" {
		if err := validateBrowserStartupProfile(filepath.Join(artifacts, "startup.json")); err != nil {
			t.Error(err)
		}
	} else if mode == "trace" {
		if info, err := os.Stat(filepath.Join(artifacts, "syscalls.log")); err != nil {
			t.Error("startup trace unavailable:", err)
		} else if info.Size() == 0 {
			t.Error("startup trace is empty")
		}
	}
	t.Logf("%s: readiness=%s, elapsed=%s, result=%v", mode, readyElapsed, time.Since(started), result)
}

func browserStartupCommand(ctx context.Context, mode, browser, profile, artifacts, url string) (*exec.Cmd, error) {
	args := []string{"--headless", "--no-remote", "--profile", profile, "--screenshot", filepath.Join(artifacts, "page.png"), url}
	var cmd *exec.Cmd
	switch mode {
	case "baseline", "profile":
		cmd = exec.CommandContext(ctx, browser, args...)
	case "trace":
		// Decode file paths, not read buffers, socket traffic, or exec environments.
		// strace records syscalls, not the time spent resolving mmap page faults.
		traceArgs := []string{"-f", "-ttt", "-T", "-y", "-s", "256", "-o", filepath.Join(artifacts, "syscalls.log"),
			"-e", "trace=openat,close,read,pread64,newfstatat,fstat,statx,access,faccessat,readlink,readlinkat,getdents64,mmap,munmap,madvise,futex,poll,ppoll,epoll_wait,connect",
			"-e", "raw=read,pread64", "--", browser}
		cmd = exec.CommandContext(ctx, "strace", append(traceArgs, args...)...)
	default:
		return nil, fmt.Errorf("unknown browser startup mode %q", mode)
	}

	// Do not accidentally profile a baseline run through inherited settings.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "MOZ_PROFILER_") && !strings.HasPrefix(entry, "MOZ_BASE_PROFILER_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if mode == "profile" {
		// MOZ_PROFILER_STARTUP starts both BaseProfiler and Gecko Profiler.
		cmd.Env = append(cmd.Env,
			"MOZ_PROFILER_STARTUP=1",
			"MOZ_PROFILER_STARTUP_ENTRIES=16777216",
			"MOZ_PROFILER_STARTUP_INTERVAL=5",
			"MOZ_PROFILER_STARTUP_FEATURES=stackwalk,js,fileioall,processcpu",
			"MOZ_PROFILER_STARTUP_FILTERS=GeckoMain,Compositor,Renderer",
			"MOZ_PROFILER_SHUTDOWN="+filepath.Join(artifacts, "startup.json"))
	}
	// Stop the tracer and browser descendants together if observation times out.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

// The command's context bounds the wait after the readiness threshold.
func observeBrowserStartup(exited <-chan error, limit time.Duration, atLimit func()) error {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-exited:
		return err
	case <-timer.C:
		atLimit()
		return <-exited
	}
}

func browserStartupResult(started, ready time.Time, limit time.Duration, exitErr, contextErr error) error {
	var readinessErr error
	if ready.IsZero() {
		readinessErr = errors.New("browser did not report JavaScript readiness")
	} else if elapsed := ready.Sub(started); elapsed > limit {
		readinessErr = fmt.Errorf("browser readiness took %s, exceeding %s", elapsed, limit)
	}
	return errors.Join(readinessErr, exitErr, contextErr)
}

func validateBrowserStartupProfile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("startup profile unavailable: %w", err)
	}
	defer file.Close()
	// Require a complete JSON document with samples, not just a created file.
	data, err := io.ReadAll(io.LimitReader(file, 64*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 64*1024*1024 {
		return errors.New("startup profile exceeds the 64 MB validation limit")
	}
	var profile struct {
		Threads []struct {
			Samples struct{ Data []json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		return fmt.Errorf("invalid startup profile: %w", err)
	}
	for _, thread := range profile.Threads {
		if len(thread.Samples.Data) > 0 {
			return nil
		}
	}
	return errors.New("startup profile contains no parent-process samples")
}
