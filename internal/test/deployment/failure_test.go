package deployment

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Keep this separate from the soak. It deliberately interrupts dependencies.
func TestDeploymentFailures(t *testing.T) {
	dir, c := deployment(t)
	command(t, dir, "docker", "compose", "up", "-d", "--wait")
	// Cleanup uses its own deadline because testing cancels t.Context first.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := localCommand(ctx, dir, "docker", "compose", "unpause", "mail")
		_ = cmd.Run()
		// A pause can leave Docker's last health result unhealthy until the next
		// interval. Restart the disposable mail process to reset that state.
		cmd = localCommand(ctx, dir, "docker", "compose", "restart", "mail")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restart Mailpit: %v: %s", err, out)
			return
		}
		cmd = localCommand(ctx, dir, "docker", "compose", "up", "-d", "--wait", "app")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restart rehearsal: %v: %s", err, out)
		}
	})

	command(t, dir, "docker", "compose", "stop", "app")
	response, _ := request(t, c, "/auth/reset/email?token=private-rehearsal-token", nil, false, false)
	if response.StatusCode != 502 {
		t.Fatal("stopped upstream did not report failure")
	}
	if logs := command(t, dir, "docker", "compose", "logs", "edge"); strings.Contains(logs, "private-rehearsal-token") {
		t.Fatal("proxy failure logged a recovery token")
	}
	cmd := localCommand(t.Context(), dir, "docker", "compose", "run", "--rm", "--no-deps", "-e", "HYPERSERVER_DATABASE_CONNECTION=/missing/storage.db", "app")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Application lifecycle failed") {
		t.Fatalf("failed startup not reported: %v %s", err, out)
	}
	command(t, dir, "docker", "compose", "up", "-d", "--wait", "app")

	// A missing required table must take readiness down without killing liveness.
	command(t, dir, "docker", "compose", "run", "--rm", "--no-deps", "maintenance", "sqlite3", "/data/hyperserver.db", "ALTER TABLE session RENAME TO unavailable_session;")
	renamed := true
	t.Cleanup(func() {
		if !renamed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := localCommand(ctx, dir, "docker", "compose", "run", "--rm", "--no-deps", "maintenance", "sqlite3", "/data/hyperserver.db", "ALTER TABLE unavailable_session RENAME TO session;")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restore session table: %v: %s", err, out)
		}
	})
	response, _ = request(t, c, "/health/ready", nil, false, false)
	// Restore before checking assertions so even a failed assertion leaves usable storage.
	command(t, dir, "docker", "compose", "run", "--rm", "--no-deps", "maintenance", "sqlite3", "/data/hyperserver.db", "ALTER TABLE unavailable_session RENAME TO session;")
	renamed = false
	if response.StatusCode != 503 {
		t.Fatal("storage outage reported ready", response.StatusCode)
	}
	response, _ = request(t, c, "/health/live", nil, false, false)
	if response.StatusCode != 204 {
		t.Fatal("storage outage killed liveness")
	}

	email := "slow-mail-" + time.Now().Format("150405.000000000") + "@example.invalid"
	password := "Local-Rehearsal-Password1!"
	response, body := request(t, c, "/auth/register/email", url.Values{"email": {email}, "password": {password}, "passwordMatch": {password}}, false, false)
	if response.StatusCode != 303 {
		t.Fatalf("registration: %d %s", response.StatusCode, body)
	}
	command(t, dir, "docker", "compose", "pause", "mail")
	form := url.Values{"email": {email}}
	r, err := http.NewRequestWithContext(t.Context(), "POST", origin+"/auth/reset/request/email", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	type result struct {
		status       int
		err          error
		elapsed      time.Duration
		requestID    string
		acknowledged bool
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		response, err := c.Do(r)
		value := result{err: err, elapsed: time.Since(start)}
		if response != nil {
			value.status = response.StatusCode
			body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
			response.Body.Close()
			if value.err == nil {
				value.err = readErr
			}
			value.requestID = response.Header.Get("X-Request-ID")
			value.acknowledged = len(body) <= maxResponseBytes && strings.Contains(string(body), "Password reset request received.")
		}
		value.elapsed = time.Since(start)
		done <- value
	}()
	// Wait for the request's SMTP connection, not a guessed handler startup delay.
	connected := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		tcp := command(t, dir, "docker", "compose", "exec", "-T", "app", "sh", "-c", "awk '$3 == \"0100007F:0401\" && $4 == \"01\" {print \"connected\"}' /proc/net/tcp")
		if strings.Contains(tcp, "connected") {
			connected = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !connected {
		t.Fatal("reset request never reached SMTP")
	}
	command(t, dir, "docker", "compose", "stop", "app")
	value := <-done
	if value.err != nil || value.status != 200 || !value.acknowledged || value.elapsed < 5*time.Second || value.elapsed > 15*time.Second {
		t.Fatalf("request did not drain through SMTP timeout: %+v", value)
	}
	t.Logf("SMTP failure acknowledged after %s while shutdown drained the request", value.elapsed)
	logs := command(t, dir, "docker", "compose", "logs", "--since", start.Format(time.RFC3339Nano), "app")
	observed := false
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, "password reset instructions could not be sent") && strings.Contains(line, `"requestID":"`+value.requestID+`"`) {
			observed = true
		}
	}
	if !observed || value.requestID == "" || strings.Contains(logs, email) || strings.Contains(logs, password) {
		t.Fatal("mail failure lost correlation or exposed credentials")
	}
}
