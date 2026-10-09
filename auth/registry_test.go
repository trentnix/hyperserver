package auth

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
)

type registryProvider struct {
	AuthService
	loaded bool
}

func (*registryProvider) AuthType() string { return "test" }
func (p *registryProvider) IsLoaded() bool { return p.loaded }

func registryConfig() *config.Config {
	c := &config.Config{Auth: config.AuthConfig{Enabled: true, JwtKey: "test-key", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"test": {"enabled": "true"}}}}
	c.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	return c
}

func TestRegistryIsolation(t *testing.T) {
	catalog := []Descriptor{{Name: "test", New: func() AuthService { return new(registryProvider) }}}
	var workers sync.WaitGroup
	registries := make(chan *Registry, 20)
	for i := 0; i < cap(registries); i++ {
		workers.Go(func() {
			r, err := NewRegistry(registryConfig(), catalog)
			if err != nil {
				t.Error(err)
				return
			}
			if len(r.Loaded()) != 0 {
				t.Error("unloaded provider selected")
			}
			r.services[0].(*registryProvider).loaded = true
			registries <- r
		})
	}
	workers.Wait()
	close(registries)
	seen := make(map[AuthService]bool)
	for r := range registries {
		services := r.Loaded()
		if len(services) != 1 || seen[services[0]] {
			t.Fatal("applications shared providers")
		}
		seen[services[0]] = true
		services[0] = nil
		copy := r.Services()
		copy[0] = nil
		if r.services[0] == nil {
			t.Fatal("returned list exposed registry storage")
		}
	}
	if len(seen) != 20 {
		t.Fatal("not all applications got providers")
	}
	var absent *Registry
	if len(absent.Loaded()) != 0 {
		t.Fatal("nil registry returned providers")
	}
}

func TestRegistryDisabledProvidersDoNotRunFactories(t *testing.T) {
	calls := 0
	catalog := []Descriptor{
		{Name: "test", New: func() AuthService { calls++; return new(registryProvider) }},
		{Name: "unused", New: func() AuthService { t.Error("disabled factory ran"); return nil }},
	}
	for _, enabled := range []bool{false, true, false, true} {
		c := registryConfig()
		c.Auth.Enabled = enabled
		r, err := NewRegistry(c, catalog)
		if err != nil {
			t.Fatal(err)
		}
		if enabled != (len(r.Services()) == 1) {
			t.Fatal("wrong enabled provider selection")
		}
	}
	if calls != 2 {
		t.Fatalf("factory calls=%d", calls)
	}
	if len(catalog) != 2 {
		t.Fatal("configuration changed catalog")
	}
}

func TestRegistryFactoryFailures(t *testing.T) {
	for _, factory := range []func() AuthService{func() AuthService { return nil }, func() AuthService { return configTestAuthService{name: "wrong"} }} {
		if r, err := NewRegistry(registryConfig(), []Descriptor{{Name: "test", New: factory}}); r != nil || err == nil {
			t.Fatalf("registry=%v error=%v", r, err)
		}
	}
	if r, err := NewRegistry(registryConfig(), nil); r != nil || err == nil || !strings.Contains(err.Error(), "unknown enabled") {
		t.Fatalf("empty local catalog: %v %v", r, err)
	}
}

func TestRegistryRejectsInvalidDescriptorsBeforeFactories(t *testing.T) {
	for _, invalid := range []Descriptor{
		{Name: "test"},
		{New: func() AuthService { return new(registryProvider) }},
		{Name: " test ", New: func() AuthService { return new(registryProvider) }},
	} {
		catalog := []Descriptor{
			{Name: "test", New: func() AuthService { t.Error("factory ran before validation"); return nil }},
			invalid,
		}
		if r, err := NewRegistry(registryConfig(), catalog); r != nil || err == nil {
			t.Fatalf("invalid descriptor accepted: %+v", invalid)
		}
	}
}

func TestRegisteredAuthSnapshot(t *testing.T) {
	name := t.TempDir()
	Register(Descriptor{Name: name, New: func() AuthService {
		t.Error("catalog access ran a factory")
		return nil
	}})
	snapshot := Registered()
	snapshot[len(snapshot)-1].New = nil
	if got := Registered(); got[len(got)-1].New == nil {
		t.Fatal("snapshot changed import catalog")
	}
}
