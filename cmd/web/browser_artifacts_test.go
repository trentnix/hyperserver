package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Keep diagnostic files outside t.TempDir only when explicitly requested.
// Browser profiles remain temporary and are never uploaded wholesale.
func browserArtifactDirectory(t *testing.T) string {
	t.Helper()
	root := os.Getenv("HS_TEST_BROWSER_ARTIFACTS")
	if root == "" {
		return t.TempDir()
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "browser-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeBrowserArtifact(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
		t.Errorf("save browser diagnostic %s: %v", name, err)
	}
}

func browserProcessSnapshot() string {
	var report strings.Builder
	// Do not collect process arguments or environments, which can contain secrets.
	for _, args := range [][]string{
		{"ps", "-eo", "pid,ppid,stat,pcpu,pmem,rss,etimes,comm", "--forest"},
		{"free", "-m"},
	} {
		report.WriteString(browserCommandSnapshot(args...))
	}
	return report.String()
}

func browserCommandSnapshot(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.CombinedOutput()
	return fmt.Sprintf("%s (error: %v):\n%s\n", strings.Join(args, " "), err, output)
}

func browserWaitSnapshot(pid int) string {
	if runtime.GOOS != "linux" {
		return "Browser thread waits and I/O counters: available only on Linux\n"
	}
	// Collect the browser parent's threads, without arguments or environments.
	// wchan identifies a sleeping thread's kernel wait location when permitted.
	threads := browserCommandSnapshot("ps", "-L", "-p", fmt.Sprint(pid), "-o", "pid,lwp,stat,wchan:32,comm")
	path := fmt.Sprintf("/proc/%d/io", pid)
	counters, err := os.ReadFile(path)
	// A process can exit during capture, and the runner can restrict /proc access.
	// Keep those errors in the report without replacing the original test failure.
	return fmt.Sprintf("Browser thread waits:\n%s\nBrowser process I/O counters, %s (error: %v):\n%s\n", threads, path, err, counters)
}

func browserFailureSnapshot(pid int, pageURL, profile, logPath, requests string) string {
	// Capture processes first. The independent probe must not replace the browser's
	// traffic history or make a failed browser check pass.
	captured := time.Now()
	waits := browserWaitSnapshot(pid)
	processes := browserProcessSnapshot()
	return fmt.Sprintf("Captured before browser cancellation at %s\n%s\n%s\nIndependent server probe: %s\n%s",
		captured.Format(time.RFC3339Nano), waits, processes, probeBrowserServer(pageURL), browserDiagnostics(profile, logPath, requests))
}

func probeBrowserServer(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil {
		return err.Error()
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "/test/diagnostics/health", "", "", ""
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err.Error()
	}
	// Reach the local listener directly, regardless of runner proxy settings.
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return err.Error()
	}
	defer response.Body.Close()
	return response.Status
}

func preserveBrowserCrashes(t *testing.T, profile, artifacts string) {
	t.Helper()
	// These are synthetic test profiles. Copy only crash diagnostics, not cookies,
	// saved credentials, or other profile data. Cap copied data at 32 MB per run.
	var remaining int64 = 32 * 1024 * 1024
	for _, subdir := range []string{"minidumps", "crashes"} {
		err := filepath.WalkDir(filepath.Join(profile, subdir), func(path string, entry os.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.Type().IsRegular() || (filepath.Ext(path) != ".dmp" && filepath.Ext(path) != ".extra") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > remaining {
				t.Logf("Skipping crash report %s: exceeds remaining artifact budget", entry.Name())
				return nil
			}
			source, err := os.Open(path)
			if err != nil {
				return err
			}
			defer source.Close()
			data, err := io.ReadAll(io.LimitReader(source, remaining+1))
			if err != nil {
				return err
			}
			if int64(len(data)) > remaining {
				return fmt.Errorf("crash report exceeded artifact budget: %s", entry.Name())
			}
			remaining -= int64(len(data))
			relative, err := filepath.Rel(profile, path)
			if err != nil {
				return err
			}
			destination := filepath.Join(artifacts, relative)
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return err
			}
			return os.WriteFile(destination, data, 0600)
		})
		if err != nil {
			t.Errorf("save Firefox crash reports: %v", err)
		}
	}
}

func TestBrowserArtifactDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HS_TEST_BROWSER_ARTIFACTS", root)
	first, second := browserArtifactDirectory(t), browserArtifactDirectory(t)
	if first == second || filepath.Dir(first) != root || filepath.Dir(second) != root {
		t.Fatalf("artifact directories must be unique children of %s: %s, %s", root, first, second)
	}
	writeBrowserArtifact(t, first, "report.txt", "diagnostics")
	contents, err := os.ReadFile(filepath.Join(first, "report.txt"))
	if err != nil || string(contents) != "diagnostics" {
		t.Fatalf("saved report=%q, error=%v", contents, err)
	}
	t.Setenv("HS_TEST_BROWSER_ARTIFACTS", "")
	if dir := browserArtifactDirectory(t); filepath.Dir(dir) == root {
		t.Fatal("unset artifact directory reused the configured directory")
	}
}

func TestPreserveBrowserCrashes(t *testing.T) {
	profile, artifacts := t.TempDir(), t.TempDir()
	// Missing crash directories are normal on successful runs.
	preserveBrowserCrashes(t, profile, artifacts)
	for _, name := range []string{"cookies.sqlite", "minidumps/crash.dmp", "minidumps/crash.extra", "minidumps/private.txt", "crashes/pending/second.extra"} {
		path := filepath.Join(profile, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(profile, "cookies.sqlite"), filepath.Join(profile, "minidumps", "linked.dmp")); err != nil {
		t.Fatal(err)
	}
	oversized, err := os.Create(filepath.Join(profile, "minidumps", "large.dmp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := oversized.Truncate(33 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := oversized.Close(); err != nil {
		t.Fatal(err)
	}
	preserveBrowserCrashes(t, profile, artifacts)
	for _, name := range []string{"minidumps/crash.dmp", "minidumps/crash.extra", "crashes/pending/second.extra"} {
		data, err := os.ReadFile(filepath.Join(artifacts, name))
		if err != nil || string(data) != name {
			t.Fatalf("crash artifact %s=%q, error=%v", name, data, err)
		}
	}
	for _, name := range []string{"cookies.sqlite", "minidumps/private.txt", "minidumps/linked.dmp", "minidumps/large.dmp"} {
		if _, err := os.Stat(filepath.Join(artifacts, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected artifact %s: %v", name, err)
		}
	}
}

func TestBrowserFailureArtifacts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checks the Linux CI process snapshot")
	}
	if os.Getenv("HS_BROWSER_FAILURE_HELPER") == "1" {
		browser := filepath.Join(t.TempDir(), "fake-firefox")
		if err := os.WriteFile(browser, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HS_TEST_FIREFOX", browser)
		result := make(chan string, 1)
		result <- "deliberate browser failure"
		runBrowser(t, "http://127.0.0.1:1", make(chan struct{}), result, func() string { return "GET /original-browser-request" })
		return
	}

	root := t.TempDir()
	t.Setenv("HS_TEST_BROWSER_ARTIFACTS", root)
	t.Setenv("HS_BROWSER_FAILURE_HELPER", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBrowserFailureArtifacts$")
	output, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() != nil || !strings.Contains(string(output), "deliberate browser failure") {
		t.Fatalf("failure helper: error=%v, context=%v\n%s", err, ctx.Err(), output)
	}
	dirs, err := os.ReadDir(root)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("artifact directories=%v, error=%v", dirs, err)
	}
	dir := filepath.Join(root, dirs[0].Name())
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	timing := read("timing.txt")
	pid := regexp.MustCompile(`Browser PID: (\d+)`).FindStringSubmatch(timing)
	if len(pid) != 2 {
		t.Fatal("missing browser PID:", timing)
	}
	processRow := regexp.MustCompile(`(?m)^\s*` + pid[1] + `\s+\d+\s+`)
	before, after := read("before-stop.txt"), read("after-stop.txt")
	if !processRow.MatchString(before) || processRow.MatchString(after) {
		t.Fatalf("browser must be alive before capture and reaped afterward\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, want := range []string{
		"Captured before browser cancellation",
		"Independent server probe:",
		"GET /original-browser-request",
		"Browser thread waits:",
		"WCHAN",
		"/proc/" + pid[1] + "/io (error: <nil>)",
		"read_bytes:",
		"write_bytes:",
	} {
		if !strings.Contains(before, want) {
			t.Errorf("failure snapshot missing %q", want)
		}
	}
	read("launch.txt")
	read("firefox.log")
}

func TestBrowserSuccessArtifacts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checks Linux process diagnostics")
	}
	root := t.TempDir()
	t.Setenv("HS_TEST_BROWSER_ARTIFACTS", root)
	browser := filepath.Join(t.TempDir(), "fake-firefox")
	if err := os.WriteFile(browser, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HS_TEST_FIREFOX", browser)
	ready := make(chan struct{})
	close(ready)
	result := make(chan string, 1)
	result <- "PASS"
	runBrowser(t, "http://127.0.0.1:1", ready, result, func() string { return "GET /successful-check" })

	dirs, err := os.ReadDir(root)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("artifact directories=%v, error=%v", dirs, err)
	}
	dir := filepath.Join(root, dirs[0].Name())
	completed, err := os.ReadFile(filepath.Join(dir, "completed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Captured after browser checks at", "Browser thread waits:", "/io (error: <nil>)", "read_bytes:", "write_bytes:"} {
		if !strings.Contains(string(completed), want) {
			t.Errorf("success snapshot missing %q:\n%s", want, completed)
		}
	}
	timing, err := os.ReadFile(filepath.Join(dir, "timing.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_, start, found := strings.Cut(string(timing), "Started at: ")
	start, _, _ = strings.Cut(start, "\n")
	if _, err := time.Parse(time.RFC3339Nano, start); !found || err != nil {
		t.Fatalf("missing or invalid launch timestamp: %s", timing)
	}
	if _, err := os.Stat(filepath.Join(dir, "before-stop.txt")); !os.IsNotExist(err) {
		t.Fatalf("successful checks must not create a failure snapshot: %v", err)
	}
}

func TestBrowserWaitSnapshotUnavailableProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("checks Linux process diagnostics")
	}
	// This value exceeds Linux's maximum PID, so it cannot select a live process.
	report := browserWaitSnapshot(2147483647)
	for _, want := range []string{"Browser thread waits:", "exit status", "/proc/2147483647/io", "no such file or directory"} {
		if !strings.Contains(report, want) {
			t.Errorf("snapshot missing %q:\n%s", want, report)
		}
	}
}
