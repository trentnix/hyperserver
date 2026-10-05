package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type routeFailureModule struct {
	initialized bool
	routeErr    error
}

func (m *routeFailureModule) Init(context.Context, *server.ApplicationServer) error {
	m.initialized = true
	return nil
}

func (m *routeFailureModule) Routes(*routing.Routes) error {
	return m.routeErr
}

func TestSetupHandlersRejectsInvalidRatePolicies(t *testing.T) {
	// The current module catalog holds shared instances. Restore the test's
	// replacement before another test initializes the reference application.
	catalog := handlers.GetHandlers()
	if len(catalog) == 0 {
		t.Fatal("reference application has no registered modules")
	}
	original := catalog[0]
	t.Cleanup(func() { catalog[0] = original })
	policyErr := errors.New("invalid module rate policy")

	for _, invalidDefault := range []bool{false, true} {
		t.Run(map[bool]string{false: "module", true: "application"}[invalidDefault], func(t *testing.T) {
			module := &routeFailureModule{routeErr: policyErr}
			catalog[0] = module
			app := &server.ApplicationServer{Web: http.NewServeMux(), Config: &config.Config{}}
			app.Config.HTTP.DefaultRateLimit.Enabled = invalidDefault
			err := SetupHandlers(context.Background(), app, nil)
			if invalidDefault {
				if err == nil || !strings.Contains(err.Error(), "http.defaultRateLimit") || module.initialized {
					t.Fatalf("invalid default: error=%v initialized=%t", err, module.initialized)
				}
			} else if !errors.Is(err, policyErr) || !strings.Contains(err.Error(), "routeFailureModule") || !module.initialized {
				t.Fatalf("module registration: error=%v initialized=%t", err, module.initialized)
			}
		})
	}
}
