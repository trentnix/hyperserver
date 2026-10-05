// Package ratelimit provides bounded, per-client HTTP request budgets.
// Policies are settings. Each Limiter owns an independent set of counters.
package ratelimit

import (
	"container/list"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/trentnix/hyperserver/pkg/services/logger"
)

// Policy specifies a fixed-window allowance per direct client IP.
// Inheriting a Policy does not share counters. All fields must be positive.
type Policy struct {
	Requests   int
	Window     time.Duration
	MaxClients int
}

// Validate rejects settings that cannot enforce a bounded request allowance.
func (p Policy) Validate() error {
	if p.Requests <= 0 || p.Window <= 0 || p.MaxClients <= 0 {
		return errors.New("rate limit requests, window, and client capacity must be positive")
	}
	return nil
}

// Limiter bounds requests per direct client IP in fixed windows starting
// with each client's first request. Reuse Handler to share a budget across routes.
// State is local to this instance and is lost on restart. Forwarding headers are
// ignored. Clients behind the same proxy or NAT share a budget.
//
// Expired entries are removed during requests. At capacity, new clients are
// rejected until an entry expires. Active entries are never evicted to admit a
// new client. No background goroutine or shutdown hook is needed.
type Limiter struct {
	mu          sync.Mutex
	name        string
	policy      Policy
	clients     map[netip.Addr]*list.Element
	expirations list.List
}

type rateWindow struct {
	ip      netip.Addr
	expires time.Time
	count   int
}

// New creates an independent limiter. name identifies this budget in rejection
// logs and must be a nonempty, static label, never a submitted value or secret.
// Reuse the returned instance only when routes should share counters.
func New(name string, policy Policy) (*Limiter, error) {
	if name == "" {
		return nil, errors.New("rate limit name must not be empty")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Limiter{
		name: name, policy: policy,
		clients: make(map[netip.Addr]*list.Element),
	}, nil
}

// Handler returns middleware that rejects excess requests with 429 and a
// Retry-After delay in seconds. Ordinary and HTMX requests receive the same plain
// text response. Admission happens before next reads input or looks up accounts.
// A missing limiter returns 500. An invalid RemoteAddr returns 400.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil || l.clients == nil {
			http.Error(w, "Rate limiter is not configured", http.StatusInternalServerError)
			return
		}
		peer, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "Invalid client address", http.StatusBadRequest)
			return
		}
		if retry, reason := l.admit(peer.Addr().Unmap().WithZone("")); retry > 0 {
			seconds := int64(retry / time.Second)
			if retry%time.Second != 0 {
				seconds++
			}
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			w.Header().Set("Cache-Control", "no-store")
			if log := logger.Get(r.Context()); log != nil && *log != nil {
				(*log).Warn("Request rate limited",
					logger.Field{Key: "limiter", Value: l.name},
					logger.Field{Key: "reason", Value: reason},
					logger.Field{Key: "requests", Value: l.policy.Requests},
					logger.Field{Key: "window", Value: l.policy.Window.String()},
					logger.Field{Key: "maxClients", Value: l.policy.MaxClients},
					logger.Field{Key: "retryAfter", Value: seconds},
				)
			}
			http.Error(w, "Too many requests. Please try again later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) admit(ip netip.Addr) (time.Duration, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	// Fixed windows expire in insertion order. Rejected requests do not extend them.
	for front := l.expirations.Front(); front != nil; front = l.expirations.Front() {
		entry := front.Value.(*rateWindow)
		if now.Before(entry.expires) {
			break
		}
		delete(l.clients, entry.ip)
		l.expirations.Remove(front)
	}
	if element := l.clients[ip]; element != nil {
		entry := element.Value.(*rateWindow)
		if entry.count >= l.policy.Requests {
			return entry.expires.Sub(now), "allowance exhausted"
		}
		entry.count++
		return 0, ""
	}
	if len(l.clients) >= l.policy.MaxClients {
		return l.expirations.Front().Value.(*rateWindow).expires.Sub(now), "client capacity reached"
	}

	entry := &rateWindow{ip: ip, expires: now.Add(l.policy.Window), count: 1}
	l.clients[ip] = l.expirations.PushBack(entry)
	return 0, ""
}
