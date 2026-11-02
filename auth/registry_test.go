package auth

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type registryProvider struct {
	AuthService
}

type configuredRegistryProvider struct {
	registryProvider
	dependencies Dependencies
}

func TestRegistryKeepsDependenciesLocal(t *testing.T) {
	catalog := []Descriptor{{Name: "test", New: func(d Dependencies) AuthService {
		return &configuredRegistryProvider{dependencies: d}
	}}}
	for _, email := range []string{"first@example.com", "second@example.com"} {
		t.Run(email, func(t *testing.T) {
			t.Parallel()
			dependencies := Dependencies{
				Accounts: &struct{ user.AccountRepository }{},
				Sessions: new(session.SessionManager),
				Policy: Policy{AllowAccount: func(_ context.Context, candidate string) (bool, error) {
					return candidate == email, nil
				}},
			}
			registry, err := NewRegistry(registryConfig(), catalog, dependencies)
			if err != nil {
				t.Fatal(err)
			}
			got := registry.Services()[0].(*configuredRegistryProvider).dependencies
			if got.Accounts != dependencies.Accounts || got.Sessions != dependencies.Sessions {
				t.Fatal("provider received another application's services")
			}
			for _, candidate := range []string{"first@example.com", "second@example.com"} {
				allowed, err := got.Policy.AllowAccount(t.Context(), candidate)
				if err != nil || allowed != (candidate == email) {
					t.Fatal("provider received another application's policy")
				}
			}
		})
	}
}

func (*registryProvider) AuthType() string { return "test" }

func registryConfig() *config.Config {
	c := &config.Config{Auth: config.AuthConfig{Enabled: true, JwtKey: "test-key000000000000000000000000", VerificationTokenExpiration: time.Hour, ResetTokenExpiration: time.Hour, Services: map[string]map[string]string{"test": {"enabled": "true"}}}}
	c.HTTP.Session.Types = map[string]string{"default": "sqliteStore"}
	return c
}

func TestRegistryIsolation(t *testing.T) {
	catalog := []Descriptor{{Name: "test", New: func(_ Dependencies) AuthService { return new(registryProvider) }}}
	var workers sync.WaitGroup
	registries := make(chan *Registry, 20)
	for i := 0; i < cap(registries); i++ {
		workers.Go(func() {
			r, err := NewRegistry(registryConfig(), catalog, Dependencies{})
			if err != nil {
				t.Error(err)
				return
			}
			if len(r.Services()) != 1 {
				t.Error("enabled provider was not selected")
			}
			registries <- r
		})
	}
	workers.Wait()
	close(registries)
	seen := make(map[AuthService]bool)
	for r := range registries {
		services := r.Services()
		if len(services) != 1 || seen[services[0]] {
			t.Fatal("applications shared providers")
		}
		seen[services[0]] = true
		services[0] = nil
		if r.services[0] == nil {
			t.Fatal("returned list exposed registry storage")
		}
	}
	if len(seen) != 20 {
		t.Fatal("not all applications got providers")
	}
	var absent *Registry
	if len(absent.Services()) != 0 {
		t.Fatal("nil registry returned providers")
	}
}

func TestRegistryDisabledProvidersDoNotRunFactories(t *testing.T) {
	calls := 0
	catalog := []Descriptor{
		{Name: "test", New: func(_ Dependencies) AuthService { calls++; return new(registryProvider) }},
		{Name: "unused", New: func(_ Dependencies) AuthService { t.Error("disabled factory ran"); return nil }},
	}
	for _, enabled := range []bool{false, true, false, true} {
		c := registryConfig()
		c.Auth.Enabled = enabled
		c.Auth.Services["unused"] = map[string]string{"enabled": "false"}
		r, err := NewRegistry(c, catalog, Dependencies{})
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
	for _, tc := range []struct {
		name    string
		factory func(_ Dependencies) AuthService
	}{
		{"nil", func(_ Dependencies) AuthService { return nil }},
		{"typed nil with nil-safe method", func(_ Dependencies) AuthService { return (*registryProvider)(nil) }},
		{"typed nil with value method", func(_ Dependencies) AuthService { return (*configTestAuthService)(nil) }},
		{"wrong type", func(_ Dependencies) AuthService { return configTestAuthService{name: "wrong"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := registryConfig()
			c.Auth.Services["later"] = map[string]string{"enabled": "true"}
			catalog := []Descriptor{
				{Name: "test", New: tc.factory},
				{Name: "later", New: func(_ Dependencies) AuthService {
					t.Fatal("factory ran after invalid provider")
					return nil
				}},
			}
			if r, err := NewRegistry(c, catalog, Dependencies{}); r != nil || err == nil || !strings.Contains(err.Error(), `auth provider "test" returned no instance or a different AuthType`) {
				t.Fatalf("registry=%v error=%v", r, err)
			}
		})
	}
	if r, err := NewRegistry(registryConfig(), nil, Dependencies{}); r != nil || err == nil || !strings.Contains(err.Error(), "unknown enabled") {
		t.Fatalf("empty local catalog: %v %v", r, err)
	}
}

func TestRegistryAcceptsValueProvider(t *testing.T) {
	r, err := NewRegistry(registryConfig(), []Descriptor{{Name: "test", New: func(_ Dependencies) AuthService {
		return configTestAuthService{name: "test"}
	}}}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if services := r.Services(); len(services) != 1 || services[0].AuthType() != "test" {
		t.Fatalf("services = %v", services)
	}
}

func TestRegistryRejectsInvalidDescriptorsBeforeFactories(t *testing.T) {
	for _, invalid := range []Descriptor{
		{Name: "test"},
		{New: func(_ Dependencies) AuthService { return new(registryProvider) }},
		{Name: " test ", New: func(_ Dependencies) AuthService { return new(registryProvider) }},
	} {
		catalog := []Descriptor{
			{Name: "test", New: func(_ Dependencies) AuthService { t.Error("factory ran before validation"); return nil }},
			invalid,
		}
		if r, err := NewRegistry(registryConfig(), catalog, Dependencies{}); r != nil || err == nil {
			t.Fatalf("invalid descriptor accepted: %+v", invalid)
		}
	}
}

func TestRegisteredAuthSnapshot(t *testing.T) {
	name := t.TempDir()
	Register(Descriptor{Name: name, New: func(_ Dependencies) AuthService {
		t.Error("catalog access ran a factory")
		return nil
	}})
	snapshot := Registered()
	snapshot[len(snapshot)-1].New = nil
	if got := Registered(); got[len(got)-1].New == nil {
		t.Fatal("snapshot changed import catalog")
	}
}
