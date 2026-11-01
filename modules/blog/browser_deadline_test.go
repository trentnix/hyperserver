package blog

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const (
	blogBrowserStartupTimeout = 60 * time.Second
	blogBrowserCheckTimeout   = 60 * time.Second
)

// waitForBlogBrowser gives checks their full budget after JavaScript starts.
// Later page loads must not restart that budget. A result can arrive before the
// readiness request and still proves that the browser executed the test script.
func waitForBlogBrowser(ctx context.Context, ready <-chan struct{}, result <-chan string, exited <-chan struct{}) (time.Duration, error) {
	started := time.Now()
	var readyAfter time.Duration
	phase, limit := "startup", blogBrowserStartupTimeout
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		select {
		case <-ready:
			readyAfter = time.Since(started)
			ready = nil
			phase, limit = "checks", blogBrowserCheckTimeout
			timer.Reset(limit)
		case report := <-result:
			if report != "PASS" {
				return readyAfter, fmt.Errorf("browser checks failed: %s", report)
			}
			return readyAfter, nil
		case <-exited:
			return readyAfter, fmt.Errorf("browser exited during %s", phase)
		case <-ctx.Done():
			return readyAfter, fmt.Errorf("browser %s canceled: %w", phase, ctx.Err())
		case <-timer.C:
			return readyAfter, fmt.Errorf("browser %s timed out after %s", phase, limit)
		}
	}
}

// TestBlogBrowserDeadlines uses virtual time to cover slow startup without
// extending the suite or depending on the host's scheduling speed.
func TestBlogBrowserDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name                                                     string
		readyAfter, reportAfter, exitAfter, cancelAfter, elapsed time.Duration
		report, wantErr                                          string
	}{
		{name: "success", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "PASS"},
		{name: "slow startup keeps full check budget", readyAfter: 59 * time.Second, reportAfter: 118 * time.Second, elapsed: 118 * time.Second, report: "PASS"},
		{name: "startup timeout", elapsed: 60 * time.Second, wantErr: "browser startup timed out after 1m0s"},
		{name: "check timeout", readyAfter: 5 * time.Second, elapsed: 65 * time.Second, wantErr: "browser checks timed out after 1m0s"},
		{name: "startup exit", exitAfter: time.Second, elapsed: time.Second, wantErr: "browser exited during startup"},
		{name: "check exit", readyAfter: time.Second, exitAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser exited during checks"},
		{name: "startup cancellation", cancelAfter: time.Second, elapsed: time.Second, wantErr: "browser startup canceled: context canceled"},
		{name: "check cancellation", readyAfter: time.Second, cancelAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser checks canceled: context canceled"},
		{name: "assertion failure", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, report: "Missing editor", wantErr: "browser checks failed: Missing editor"},
		{name: "empty report", readyAfter: time.Second, reportAfter: 2 * time.Second, elapsed: 2 * time.Second, wantErr: "browser checks failed:"},
		{name: "result before readiness", reportAfter: time.Second, elapsed: time.Second, report: "PASS"},
		{name: "failure before readiness", reportAfter: time.Second, elapsed: time.Second, report: "Script failed", wantErr: "browser checks failed: Script failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ready, exited := make(chan struct{}), make(chan struct{})
				result := make(chan string, 1)
				if tc.readyAfter > 0 {
					time.AfterFunc(tc.readyAfter, func() { close(ready) })
				}
				if tc.reportAfter > 0 {
					time.AfterFunc(tc.reportAfter, func() { result <- tc.report })
				}
				if tc.exitAfter > 0 {
					time.AfterFunc(tc.exitAfter, func() { close(exited) })
				}
				if tc.cancelAfter > 0 {
					time.AfterFunc(tc.cancelAfter, cancel)
				}

				started := time.Now()
				readyAfter, err := waitForBlogBrowser(ctx, ready, result, exited)
				if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				if readyAfter != tc.readyAfter || time.Since(started) != tc.elapsed {
					t.Fatalf("ready after %s, elapsed %s. Want %s, %s", readyAfter, time.Since(started), tc.readyAfter, tc.elapsed)
				}
			})
		})
	}
}
