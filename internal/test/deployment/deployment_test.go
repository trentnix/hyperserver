// These tests exercise only deploy/local. They are deliberately excluded from
// ordinary test runs. They stop/restart the disposable app and change its data.
package deployment

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const origin = "https://localhost:18443"
const maxResponseBytes = 2_000_000

func deployment(t *testing.T) (string, *http.Client) {
	t.Helper()
	if os.Getenv("HS_TEST_DEPLOYMENT") != "1" {
		t.Skip("set HS_TEST_DEPLOYMENT=1 for the disposable local deployment")
	}
	dir, err := filepath.Abs("../../../deploy/local")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile(filepath.Join(dir, "state/tls/cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cert) {
		t.Fatal("invalid rehearsal certificate")
	}
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, MaxIdleConnsPerHost: 16}
	t.Cleanup(transport.CloseIdleConnections)
	return dir, &http.Client{Transport: transport, Jar: jar, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func command(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := localCommand(t.Context(), dir, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

// Pin both the Docker context and Compose file/project so inherited shell
// settings cannot redirect a destructive rehearsal command to another stack.
func localCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	if len(args) > 1 && args[0] == "docker" {
		prefix := []string{"docker", "--context", "default"}
		if args[1] == "compose" {
			prefix = append(prefix, "compose", "--file", filepath.Join(dir, "compose.yaml"), "--env-file", filepath.Join(dir, ".env"), "--project-name", "hyperserver-rehearsal")
			args = append(prefix, args[2:]...)
		} else {
			args = append(prefix, args[1:]...)
		}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "COMPOSE_PROFILES=")
	return cmd
}

func request(t *testing.T, c *http.Client, path string, form url.Values, hx, history bool) (*http.Response, string) {
	t.Helper()
	method := http.MethodGet
	var body io.Reader
	if form != nil {
		method = http.MethodPost
		body = strings.NewReader(form.Encode())
	}
	r, err := http.NewRequestWithContext(t.Context(), method, origin+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
	}
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	if history {
		r.Header.Set("HX-History-Restore-Request", "true")
	}
	response, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxResponseBytes {
		t.Fatal("response exceeded the exercise's body limit")
	}
	return response, string(data)
}

func TestDeploymentRecovery(t *testing.T) {
	dir, c := deployment(t)
	command(t, dir, "docker", "compose", "up", "-d", "--wait")
	stamp := fmt.Sprint(time.Now().UnixNano())
	loginAdministrator(t, c)
	email, password := rehearsalEmail, rehearsalPassword
	response, body := request(t, c, "/post", url.Values{"title": {"Recovery " + stamp}, "slug": {"recovery-" + stamp}, "content": {stamp}, "published": {"on"}}, false, false)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("create post: %d %s", response.StatusCode, body)
	}

	backup := "recovery-" + stamp
	command(t, dir, "bash", "storage.sh", "backup", backup)
	// Exercise the real blog down migration. Restore, not force, is the recovery procedure.
	command(t, dir, "docker", "compose", "run", "--rm", "--no-deps", "maintenance", "migrate", "-path", "/migrations/blog", "-database", "sqlite3:///data/blog.db?x-migrations-table=blog_schema_version", "down", "1")
	command(t, dir, "bash", "storage.sh", "restore", backup)
	command(t, dir, "docker", "compose", "up", "-d", "--wait", "app")
	response, body = request(t, c, "/", nil, false, false)
	if response.StatusCode != 200 || !strings.Contains(body, "Log out") {
		t.Fatal("restored account/session did not preserve login")
	}
	count := command(t, dir, "docker", "compose", "run", "--rm", "--no-deps", "maintenance", "sqlite3", "/data/blog.db", "SELECT COUNT(*) FROM blog_posts WHERE markdown='"+stamp+"';")
	fields := strings.Fields(count)
	if len(fields) == 0 || fields[len(fields)-1] != "1" {
		t.Fatal("post was not restored:", count)
	}
	command(t, dir, "docker", "compose", "restart", "app")
	command(t, dir, "docker", "compose", "up", "-d", "--wait", "app")
	response, body = request(t, c, "/", nil, false, false)
	if response.StatusCode != 200 || !strings.Contains(body, "Log out") {
		t.Fatal("restart lost restored account/session")
	}
	checkCache(t, c, email)
	_, freshLogin := deployment(t)
	response, body = request(t, freshLogin, "/login", url.Values{"provider": {"email"}, "email": {email}, "password": {password}}, false, false)
	if response.StatusCode != 303 {
		t.Fatalf("restored credentials failed: %d %s", response.StatusCode, body)
	}
	checkCache(t, freshLogin, email)

	_, anonymous := deployment(t)
	checkCache(t, anonymous, "")
	response, body = request(t, anonymous, "/", nil, false, false)
	if strings.Contains(body, "Log out") {
		t.Fatal("shared cache leaked administrator controls")
	}

	command(t, dir, "docker", "compose", "stop", "mail")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := localCommand(ctx, dir, "docker", "compose", "up", "-d", "mail")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restore Mailpit: %v: %s", err, out)
		}
	})
	response, _ = request(t, c, "/health/ready", nil, false, false)
	if response.StatusCode != 503 {
		t.Fatal("required mail outage was not visible")
	}
	response, _ = request(t, c, "/health/live", nil, false, false)
	if response.StatusCode != 204 {
		t.Fatal("dependency outage failed liveness")
	}
	command(t, dir, "docker", "compose", "up", "-d", "mail")
}

func checkCache(t *testing.T, c *http.Client, email string) {
	t.Helper()
	for _, variant := range []struct{ hx, history bool }{{false, false}, {true, false}, {true, true}, {false, true}} {
		for range 2 {
			r, body := request(t, c, "/", nil, variant.hx, variant.history)
			cacheStatus := r.Header.Get("X-Cache-Status")
			if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" || (cacheStatus != "MISS" && cacheStatus != "BYPASS") {
				t.Fatalf("unsafe cache response: %d %v", r.StatusCode, r.Header)
			}
			if full := strings.Contains(strings.ToLower(body), "<!doctype html>"); full != (!variant.hx || variant.history) {
				t.Fatal("cache mixed page and fragment")
			}
			for _, header := range []string{"HX-Request", "HX-History-Restore-Request"} {
				if !slices.ContainsFunc(strings.Split(strings.Join(r.Header.Values("Vary"), ","), ","), func(value string) bool { return strings.EqualFold(strings.TrimSpace(value), header) }) {
					t.Fatal("missing cache variance")
				}
			}
			if email != "" && (!variant.hx || variant.history) && !strings.Contains(body, "Log out") {
				t.Fatal("lost personalized response")
			}
		}
	}
	// Demonstrate that the edge cache is active, not merely bypassed for everything.
	var hit bool
	_, anon := deployment(t)
	for range 3 {
		r, _ := request(t, anon, "/assets/css/blog.css", nil, false, false)
		hit = hit || r.Header.Get("X-Cache-Status") == "HIT"
	}
	if !hit {
		t.Fatal("edge cache never served a public asset hit")
	}
	// Unique query keys let us prove that each HTMX variant starts with its own
	// cache miss and then hits, even though this public asset has identical bytes.
	path := fmt.Sprintf("/assets/css/blog.css?cache-probe=%d", time.Now().UnixNano())
	for _, variant := range []struct{ hx, history bool }{{false, false}, {true, false}, {true, true}, {false, true}} {
		for _, want := range []string{"MISS", "HIT"} {
			r, _ := request(t, anon, path, nil, variant.hx, variant.history)
			if r.StatusCode != 200 || r.Header.Get("X-Cache-Status") != want {
				t.Fatalf("public cache key hx=%v history=%v: %v", variant.hx, variant.history, r.Header)
			}
		}
	}
}

func TestDeploymentSoak(t *testing.T) {
	dir, c := deployment(t)
	if os.Getenv("HS_TEST_SOAK") != "1" {
		t.Skip("set HS_TEST_SOAK=1 to run the optional soak")
	}
	duration := 30 * time.Minute
	if value := os.Getenv("HS_SOAK_DURATION"); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		if err != nil || duration <= 0 {
			t.Fatal("invalid HS_SOAK_DURATION")
		}
	}
	exerciseLoad(t, dir, c, duration, 400*time.Millisecond)
}

func TestDeploymentLoad(t *testing.T) {
	dir, c := deployment(t)
	exerciseLoad(t, dir, c, 30*time.Second, 0)
}

func exerciseLoad(t *testing.T, dir string, c *http.Client, duration, spacing time.Duration) {
	t.Helper()
	command(t, dir, "docker", "compose", "up", "-d", "--wait")
	_, authenticated := deployment(t)
	loginAdministrator(t, authenticated)
	clients := []*http.Client{c, c, authenticated, authenticated}
	for worker, client := range clients {
		for range 25 {
			request(t, client, "/", nil, worker%2 == 1, false)
		}
	}

	container := command(t, dir, "docker", "compose", "ps", "-q", "app")
	t.Log("initial resources:", command(t, dir, "docker", "stats", "--no-stream", "--format", "{{json .}}", container))
	// Fixed worker count and bounded reads prevent the load generator from becoming
	// an unbounded queue. Soak uses ten requests/second. Load runs without a pause.
	var failed, total, elapsed atomic.Int64
	var wg sync.WaitGroup
	deadline := time.Now().Add(duration)
	for worker, client := range clients {
		wg.Go(func() {
			var tick <-chan time.Time
			if spacing > 0 {
				ticker := time.NewTicker(spacing)
				defer ticker.Stop()
				tick = ticker.C
			}
			for time.Now().Before(deadline) {
				if t.Context().Err() != nil {
					return
				}
				if tick != nil {
					select {
					case <-t.Context().Done():
						return
					case <-tick:
					}
				}
				r, _ := http.NewRequestWithContext(t.Context(), "GET", origin+"/", nil)
				if worker%2 == 1 {
					r.Header.Set("HX-Request", "true")
				}
				start := time.Now()
				response, err := client.Do(r)
				total.Add(1)
				if err != nil {
					failed.Add(1)
					continue
				}
				size, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes+1))
				response.Body.Close()
				elapsed.Add(int64(time.Since(start)))
				if err != nil || size > maxResponseBytes || response.StatusCode != 200 {
					failed.Add(1)
				}
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	progress := time.NewTicker(time.Minute)
	defer progress.Stop()
running:
	for {
		select {
		case <-done:
			break running
		case <-progress.C:
			t.Logf("requests=%d failures=%d", total.Load(), failed.Load())
			t.Log("resources:", command(t, dir, "docker", "stats", "--no-stream", "--format", "{{json .}}", container))
		}
	}
	average := time.Duration(0)
	if total.Load() > 0 {
		average = time.Duration(elapsed.Load() / total.Load())
	}
	t.Logf("duration=%s requests=%d failures=%d mean response=%s", duration, total.Load(), failed.Load(), average)
	t.Log("final resources:", command(t, dir, "docker", "stats", "--no-stream", "--format", "{{json .}}", container))
	if failed.Load() != 0 || total.Load() == 0 {
		t.Fatal("soak requests failed")
	}
	state := command(t, dir, "docker", "inspect", "--format", "{{json .State}}", container)
	var result struct {
		OOMKilled bool
		Running   bool
	}
	if err := json.NewDecoder(bytes.NewBufferString(state)).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.OOMKilled || !result.Running {
		t.Fatal("container did not stay healthy")
	}
	response, _ := request(t, c, "/health/ready", nil, false, false)
	if response.StatusCode != 204 {
		t.Fatal("deployment was not ready after load")
	}
}

const rehearsalEmail = "admin@example.invalid"
const rehearsalPassword = "Local-Rehearsal-Password1!"

func loginAdministrator(t *testing.T, c *http.Client) {
	t.Helper()
	credentials := url.Values{"provider": {"email"}, "email": {rehearsalEmail}, "password": {rehearsalPassword}, "passwordMatch": {rehearsalPassword}}
	response, body := request(t, c, "/login", credentials, false, false)
	if response.StatusCode != http.StatusSeeOther {
		response, body = request(t, c, "/register", credentials, false, false)
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("register rehearsal administrator: %d %s", response.StatusCode, body)
		}
		token := verificationToken(t)
		response, body = request(t, c, "/auth/verify", url.Values{"token": {token}}, false, false)
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("verify administrator: %d %s", response.StatusCode, body)
		}
		response, body = request(t, c, "/login", credentials, false, false)
		if response.StatusCode != http.StatusSeeOther {
			t.Fatalf("login: %d %s", response.StatusCode, body)
		}
	}
	// Complete first-login guidance without following arbitrary external redirects.
	request(t, c, "/", nil, false, false)
	response, body = request(t, c, "/post/manage", nil, false, false)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Log out") {
		t.Fatal("administrator session was not established")
	}
}

// Read only the disposable Mailpit instance, never a real mailbox.
func verificationToken(t *testing.T) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	read := func(path string, value any) {
		t.Helper()
		response, err := client.Get("http://localhost:18025" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Mailpit: %d", response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(value); err != nil {
			t.Fatal(err)
		}
	}
	var inbox struct {
		Messages []struct {
			ID string
			To []struct{ Address string }
		}
	}
	read("/api/v1/messages", &inbox)
	for _, message := range inbox.Messages {
		for _, recipient := range message.To {
			if recipient.Address != rehearsalEmail {
				continue
			}
			var detail struct{ Text, HTML string }
			read("/api/v1/message/"+url.PathEscape(message.ID), &detail)
			match := regexp.MustCompile(`/auth/verify\?token=([A-Za-z0-9_-]+)`).FindStringSubmatch(detail.Text + " " + detail.HTML)
			if len(match) == 2 {
				return match[1]
			}
		}
	}
	t.Fatal("verification message not found in disposable Mailpit")
	return ""
}
