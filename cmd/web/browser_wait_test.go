package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitForBrowser(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		readyAfter, reportAfter, exitAfter, elapsed time.Duration
		report, wantErr                             string
	}{
		{name: "success", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "PASS"},
		{name: "slow startup leaves full check budget", readyAfter: 29 * time.Second, reportAfter: 43 * time.Second, elapsed: 43 * time.Second, report: "PASS"},
		{name: "startup timeout", elapsed: 30 * time.Second, wantErr: "browser startup timed out after 30s"},
		{name: "script failure before readiness", reportAfter: time.Second, elapsed: time.Second, report: "JavaScript error: script failed", wantErr: "browser checks failed before readiness was observed: JavaScript error: script failed"},
		{name: "success requires readiness", reportAfter: time.Second, elapsed: time.Second, report: "PASS", wantErr: "browser reported success before JavaScript reported readiness"},
		{name: "check timeout", readyAfter: 5 * time.Second, elapsed: 20 * time.Second, wantErr: "browser checks timed out after 15s"},
		{name: "startup exit", exitAfter: time.Second, elapsed: time.Second, wantErr: "browser exited during startup"},
		{name: "check exit", readyAfter: time.Second, exitAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser exited during checks"},
		{name: "assertion failure", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "HTMX did not load", wantErr: "browser checks failed: HTMX did not load"},
		{name: "empty report is not success", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser checks failed:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ready, exited := make(chan struct{}), make(chan struct{})
				result := make(chan string, 1)
				if tc.readyAfter > 0 {
					go func() {
						time.Sleep(tc.readyAfter)
						close(ready)
					}()
				}
				if tc.reportAfter > 0 {
					go func() {
						time.Sleep(tc.reportAfter)
						result <- tc.report
					}()
				}
				if tc.exitAfter > 0 {
					go func() {
						time.Sleep(tc.exitAfter)
						close(exited)
					}()
				}

				started := time.Now()
				err := waitForBrowser(ready, result, exited, 30*time.Second, 15*time.Second)
				if tc.wantErr == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v, want %q", err, tc.wantErr)
				}
				if elapsed := time.Since(started); elapsed != tc.elapsed {
					t.Fatalf("elapsed=%s, want %s", elapsed, tc.elapsed)
				}
			})
		})
	}
}

func TestWaitForBrowserAlreadyFinished(t *testing.T) {
	for range 100 {
		ready := make(chan struct{})
		close(ready)
		result := make(chan string, 1)
		result <- "PASS"
		if err := waitForBrowser(ready, result, make(chan struct{}), time.Second, time.Second); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserDiagnostics(t *testing.T) {
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("test profile"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "firefox.log")
	if err := os.WriteFile(logPath, []byte("browser startup message"), 0600); err != nil {
		t.Fatal(err)
	}
	diagnostics := browserDiagnostics(profile, logPath, "GET /test/browser")
	for _, want := range []string{profile, "prefs.js", "browser startup message", "GET /test/browser"} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q: %s", want, diagnostics)
		}
	}

	missing := filepath.Join(t.TempDir(), "missing")
	diagnostics = browserDiagnostics(missing, missing, "")
	for _, want := range []string{"Profile entries (read error:", "Browser output (read error:", "no such file or directory", "HTTP requests:"} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("missing-file diagnostics omitted %q: %s", want, diagnostics)
		}
	}
}
