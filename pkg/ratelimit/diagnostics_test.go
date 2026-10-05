package ratelimit

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type rateLog struct{ entries []map[string]any }

func (*rateLog) Debug(string, ...logger.Field)        {}
func (*rateLog) Info(string, ...logger.Field)         {}
func (*rateLog) Error(string, ...logger.Field)        {}
func (l *rateLog) With(...logger.Field) logger.Logger { return l }
func (l *rateLog) Warn(message string, fields ...logger.Field) {
	entry := map[string]any{"message": message}
	for _, field := range fields {
		entry[field.Key] = field.Value
	}
	l.entries = append(l.entries, entry)
}

func TestRateLimiterDiagnostics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l, err := New("shared mail", Policy{Requests: 1, Window: time.Minute, MaxClients: 1})
		if err != nil {
			t.Fatal(err)
		}
		capture := &rateLog{}
		var log logger.Logger = capture
		handler := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		for _, ip := range []string{"192.0.2.1:1", "192.0.2.1:2", "192.0.2.2:1"} {
			r := httptest.NewRequest("POST", "/private-token?email=private-account", strings.NewReader("private-password"))
			r.RemoteAddr = ip
			r = r.WithContext(logger.Set(r.Context(), &log))
			handler.ServeHTTP(httptest.NewRecorder(), r)
		}
		if len(capture.entries) != 2 {
			t.Fatalf("log entries=%d", len(capture.entries))
		}
		for i, reason := range []string{"allowance exhausted", "client capacity reached"} {
			entry := capture.entries[i]
			if entry["message"] != "Request rate limited" || entry["limiter"] != "shared mail" || entry["reason"] != reason || entry["requests"] != 1 || entry["window"] != "1m0s" || entry["retryAfter"] != int64(60) || entry["maxClients"] != 1 {
				t.Fatalf("incomplete rejection log: %v", entry)
			}
		}
		if strings.Contains(fmt.Sprint(capture.entries), "private") {
			t.Fatal("rejection log retained submitted values")
		}
	})
}

func TestRateLimiterPolicySnapshot(t *testing.T) {
	policy := Policy{Requests: 1, Window: time.Minute, MaxClients: 1}
	if _, err := New("", policy); err == nil {
		t.Fatal("empty diagnostic name accepted")
	}
	l, err := New("snapshot", policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Requests = 100
	h := l.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	rateRequest(h, "192.0.2.1:1")
	if w := rateRequest(h, "192.0.2.1:1"); w.Code != 429 {
		t.Fatal("caller mutation changed the limiter")
	}
}

func TestRateLimiterAgainstReferenceModel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		policy := Policy{Requests: 3, Window: time.Second, MaxClients: 4}
		l, err := New("model", policy)
		if err != nil {
			t.Fatal(err)
		}
		type window struct {
			expires time.Time
			count   int
		}
		model := make(map[netip.Addr]window)
		random := rand.New(rand.NewPCG(1, 2))
		for i := 0; i < 10000; i++ {
			if random.IntN(4) == 0 {
				time.Sleep(time.Duration(random.IntN(5)) * 250 * time.Millisecond)
			}
			now := time.Now()
			var earliest time.Time
			for ip, entry := range model {
				if !now.Before(entry.expires) {
					delete(model, ip)
					continue
				}
				if earliest.IsZero() || entry.expires.Before(earliest) {
					earliest = entry.expires
				}
			}
			ip := netip.AddrFrom4([4]byte{192, 0, 2, byte(random.IntN(8) + 1)})
			var want time.Duration
			entry, present := model[ip]
			switch {
			case present && entry.count == policy.Requests:
				want = entry.expires.Sub(now)
			case !present && len(model) == policy.MaxClients:
				want = earliest.Sub(now)
			default:
				if !present {
					entry.expires = now.Add(policy.Window)
				}
				entry.count++
				model[ip] = entry
			}
			got, _ := l.admit(ip)
			if got != want || len(l.clients) != len(model) || l.expirations.Len() != len(model) {
				t.Fatalf("step %d: retry=%v want=%v clients=%d queue=%d model=%d", i, got, want, len(l.clients), l.expirations.Len(), len(model))
			}
		}
	})
}
