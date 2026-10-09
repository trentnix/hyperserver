package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

// The consumer defines the storage contract, independently of its provider.
type greetingRepository interface {
	Greeting(context.Context) (string, error)
}

type greetingPage struct{ store greetingRepository }

func (p *greetingPage) BindDependencies(d handlers.Dependencies) error {
	var err error
	p.store, err = handlers.GetDependency[greetingRepository](d, "greetings")
	return err
}

func (*greetingPage) Init(context.Context, *server.ApplicationServer) error { return nil }

func (p *greetingPage) Routes(routes *routing.Routes) error {
	routes.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		greeting, err := p.store.Greeting(r.Context())
		if err != nil {
			http.Error(w, "Could not load greeting", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, greeting)
	})
	return routes.Err()
}

type memoryGreetings struct{}

func (*memoryGreetings) Greeting(context.Context) (string, error) { return "Hello!", nil }

type greetingStorage struct{ store *memoryGreetings }

func (s *greetingStorage) Init(context.Context, *server.ApplicationServer) error {
	s.store = &memoryGreetings{}
	return nil
}

func (s *greetingStorage) Capabilities() map[string]any {
	return map[string]any{"greetings": s.store}
}

func (*greetingStorage) Routes(*routing.Routes) error { return nil }

func ExampleInitialize() {
	catalog := []handlers.Descriptor{
		{Name: "pages", New: func() handlers.Handler { return new(greetingPage) },
			Requires: []handlers.Requirement{{Capability: "greetings"}}},
		{Name: "memory-greetings", New: func() handlers.Handler { return new(greetingStorage) },
			Provides: []string{"greetings"}},
	}
	modules, err := handlers.Initialize(context.Background(), nil, catalog, nil)
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	routes := routing.NewRoutes(mux)
	for _, module := range modules {
		if err := module.Routes(routes); err != nil {
			panic(err)
		}
		if err := routes.Err(); err != nil {
			panic(err)
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(response.Body.String())
	// Output: Hello!
}
