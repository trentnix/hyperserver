package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

func TestBrowserStartupCommand(t *testing.T) {
	t.Setenv("MOZ_PROFILER_SHUTDOWN", "/must-not-be-used.json")
	t.Setenv("MOZ_PROFILER_STARTUP_FEATURES_BITFIELD", "0")
	t.Setenv("MOZ_BASE_PROFILER_HELP", "1")
	for _, mode := range []string{"baseline", "profile", "trace"} {
		t.Run(mode, func(t *testing.T) {
			cmd, err := browserStartupCommand(t.Context(), mode, "/fake/firefox", "/fresh/profile", "/artifacts", "http://127.0.0.1:1234")
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(cmd.Args, " ")
			if !strings.Contains(args, "/fake/firefox --headless --no-remote --profile /fresh/profile --screenshot /artifacts/page.png http://127.0.0.1:1234") {
				t.Fatal("incorrect browser command:", args)
			}
			if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.Cancel == nil || cmd.WaitDelay != 2*time.Second {
				t.Fatal("missing bounded process-group cleanup")
			}
			env := strings.Join(cmd.Env, "\n")
			for _, unwanted := range []string{"must-not-be-used", "MOZ_BASE_PROFILER_HELP", "MOZ_PROFILER_STARTUP_FEATURES_BITFIELD"} {
				if strings.Contains(env, unwanted) {
					t.Errorf("inherited profiler override %s", unwanted)
				}
			}
			if mode == "profile" {
				for _, want := range []string{
					"MOZ_PROFILER_STARTUP=1",
					"MOZ_PROFILER_STARTUP_FEATURES=stackwalk,js,fileioall,processcpu",
					"MOZ_PROFILER_SHUTDOWN=/artifacts/startup.json",
				} {
					if !strings.Contains(env, want) {
						t.Errorf("missing profiler setting %s", want)
					}
				}
			} else if strings.Contains(env, "MOZ_PROFILER_") {
				t.Fatal("non-profile mode inherited profiling settings")
			}
			if mode == "trace" && (cmd.Args[0] != "strace" || !strings.Contains(args, "raw=read,pread64") || !strings.Contains(args, "/artifacts/syscalls.log")) {
				t.Fatal("trace must preserve output without decoding read buffers:", args)
			}
		})
	}
	if _, err := browserStartupCommand(t.Context(), "invalid", "firefox", "", "", ""); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestObserveBrowserStartup(t *testing.T) {
	for _, delay := range []time.Duration{5 * time.Second, 45 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				exitErr := errors.New("browser exit")
				exited := make(chan error, 1)
				go func() {
					time.Sleep(delay)
					exited <- exitErr
				}()
				started, captures := time.Now(), 0
				err := observeBrowserStartup(exited, 30*time.Second, func() {
					captures++
					if time.Since(started) != 30*time.Second {
						t.Error("snapshot did not run at the threshold")
					}
				})
				if !errors.Is(err, exitErr) || time.Since(started) != delay {
					t.Fatalf("observation did not preserve the exit: elapsed=%s, error=%v", time.Since(started), err)
				}
				wantCaptures := 0
				if delay > 30*time.Second {
					wantCaptures = 1
				}
				if captures != wantCaptures {
					t.Fatalf("captures=%d, want %d", captures, wantCaptures)
				}
			})
		})
	}
}

func TestBrowserStartupResult(t *testing.T) {
	started := time.Now()
	for _, tc := range []struct {
		name       string
		ready      time.Time
		exitErr    error
		contextErr error
		wantError  bool
	}{
		{name: "timely", ready: started.Add(time.Second)},
		{name: "at limit", ready: started.Add(30 * time.Second)},
		{name: "late success still fails", ready: started.Add(31 * time.Second), wantError: true},
		{name: "exit without readiness", wantError: true},
		{name: "failed exit after readiness", ready: started.Add(time.Second), exitErr: errors.New("exit failed"), wantError: true},
		{name: "observation timeout", contextErr: context.DeadlineExceeded, wantError: true},
		{name: "export timeout after readiness", ready: started.Add(time.Second), contextErr: context.DeadlineExceeded, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := browserStartupResult(started, tc.ready, 30*time.Second, tc.exitErr, tc.contextErr)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, want error=%v", err, tc.wantError)
			}
			for _, want := range []error{tc.exitErr, tc.contextErr} {
				if want != nil && !errors.Is(err, want) {
					t.Errorf("lost error %v: %v", want, err)
				}
			}
		})
	}
}

func TestValidateBrowserStartupProfileSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(64*1024*1024 + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateBrowserStartupProfile(path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("oversized profile was not rejected:", err)
	}
}

func TestValidateBrowserStartupProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	if err := validateBrowserStartupProfile(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing profile was not reported:", err)
	}
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"sampled", `{"threads":[{"samples":{"data":[[0,1,2]]}}]}`, true},
		{"empty", "", false},
		{"truncated", `{"threads":[`, false},
		{"no threads", `{}`, false},
		{"no samples", `{"threads":[{"samples":{"data":[]}}]}`, false},
		{"trailing data", `{"threads":[{"samples":{"data":[[]]}}]} garbage`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateBrowserStartupProfile(path); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestBrowserStartupCancellation(t *testing.T) {
	// A synthetic launcher stands in for strace and spawns a browser descendant.
	launcher := filepath.Join(t.TempDir(), "launcher")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nsleep 60 &\necho $!\nwait\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd, err := browserStartupCommand(ctx, "baseline", launcher, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Even assertion failures must terminate and reap the launcher.
	defer func() {
		cancel()
		if cmd.ProcessState == nil {
			cmd.Wait()
		}
	}()
	var child int
	if _, err := fmt.Fscan(output, &child); err != nil {
		t.Fatal(err)
	}
	if group, err := syscall.Getpgid(child); err != nil || group != cmd.Process.Pid {
		t.Fatalf("child group=%d, launcher=%d, error=%v", group, cmd.Process.Pid, err)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("canceled launcher succeeded")
	}
	// A killed orphan can remain a zombie briefly until the system reaps it.
	deadline := time.Now().Add(time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(stat)[strings.LastIndexByte(string(stat), ')')+1:])
		if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser descendant survived cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
