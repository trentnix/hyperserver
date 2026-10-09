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
	initErr     error
	routeErr    error
}

func (m *routeFailureModule) Init(context.Context, *server.ApplicationServer) error {
	m.initialized = true
	return m.initErr
}

func (m *routeFailureModule) Routes(*routing.Routes) error {
	return m.routeErr
}

func TestSetupHandlersRejectsInvalidRatePolicies(t *testing.T) {
	policyErr := errors.New("invalid module rate policy")

	for _, invalidDefault := range []bool{false, true} {
		t.Run(map[bool]string{false: "module", true: "application"}[invalidDefault], func(t *testing.T) {
			module := &routeFailureModule{routeErr: policyErr}
			catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler { return module }}}
			app := &server.ApplicationServer{Web: http.NewServeMux(), Config: &config.Config{}}
			app.Config.HTTP.DefaultRateLimit.Enabled = invalidDefault
			err := SetupHandlers(context.Background(), app, nil, catalog)
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

func TestSetupHandlersIdentifiesInitializationFailure(t *testing.T) {
	cause := errors.New("connection refused")
	module := &routeFailureModule{initErr: cause}
	catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler { return module }}}
	app := &server.ApplicationServer{Web: http.NewServeMux(), Config: &config.Config{}}
	err := SetupHandlers(context.Background(), app, nil, catalog)
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), `initialize module "test"`) {
		t.Fatalf("initialization error = %v", err)
	}
}
