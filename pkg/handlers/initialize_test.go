package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type dependencyModule struct {
	init    func(context.Context) error
	bind    func(handlers.Dependencies) error
	provide func() map[string]any
}

func (m *dependencyModule) Init(ctx context.Context, _ *server.ApplicationServer) error {
	if m.init != nil {
		return m.init(ctx)
	}
	return nil
}

func (*dependencyModule) Routes(*routing.Routes) error { panic("Initialize must not bind routes") }

func (m *dependencyModule) BindDependencies(d handlers.Dependencies) error {
	if m.bind != nil {
		return m.bind(d)
	}
	return nil
}

func (m *dependencyModule) Capabilities() map[string]any { return m.provide() }

func dependencyDescriptor(name string, module handlers.Handler) handlers.Descriptor {
	return handlers.Descriptor{Name: name, New: func() handlers.Handler { return module }}
}

type namedService interface{ Name() string }
type memoryService struct{ name string }

func (s *memoryService) Name() string { return s.name }

func TestInitializeBindsSelectedDependencies(t *testing.T) {
	for _, mode := range []string{"single", "selected", "multiple", "optional absent", "optional present"} {
		t.Run(mode, func(t *testing.T) {
			var services []namedService
			var single namedService
			var steps []string
			consumer := &dependencyModule{bind: func(d handlers.Dependencies) error {
				steps = append(steps, "bind consumer")
				if _, err := handlers.GetDependency[any](d, "private"); err == nil {
					t.Error("consumer accessed an undeclared capability")
				}
				var err error
				services, err = handlers.GetDependencies[namedService](d, "storage")
				if err != nil {
					return err
				}
				single, err = handlers.GetDependency[namedService](d, "storage")
				if mode == "multiple" {
					if err == nil {
						t.Error("single lookup accepted multiple providers")
					}
					return nil
				}
				return err
			}, init: func(context.Context) error {
				steps = append(steps, "init consumer")
				return nil
			}}
			catalog := []handlers.Descriptor{dependencyDescriptor("consumer", consumer)}
			catalog[0].Requires = []handlers.Requirement{{Capability: "storage", Multiple: mode == "multiple", Optional: strings.HasPrefix(mode, "optional")}}
			if mode != "optional absent" {
				for _, name := range []string{"one", "two"} {
					if name == "two" && mode != "selected" && mode != "multiple" {
						continue
					}
					var service *memoryService
					provider := &dependencyModule{init: func(context.Context) error {
						steps = append(steps, "init "+name)
						service = &memoryService{name: name}
						return nil
					}, provide: func() map[string]any {
						steps = append(steps, "publish "+name)
						return map[string]any{"storage": service, "private": "not declared by consumer"}
					}}
					descriptor := dependencyDescriptor(name, provider)
					descriptor.Provides = []string{"storage", "private"}
					catalog = append(catalog, descriptor)
				}
			}
			var selections map[string]string
			if mode == "selected" {
				selections = map[string]string{"storage": "two"}
			}
			instances, err := handlers.Initialize(context.Background(), nil, catalog, selections)
			if err != nil {
				t.Fatal(err)
			}
			if len(instances) != len(catalog) {
				t.Fatalf("instances = %d", len(instances))
			}
			var names []string
			for _, service := range services {
				names = append(names, service.Name())
			}
			wantNames := map[string][]string{"single": {"one"}, "selected": {"two"}, "multiple": {"one", "two"}, "optional absent": nil, "optional present": {"one"}}[mode]
			if !reflect.DeepEqual(names, wantNames) {
				t.Fatalf("services = %v, want %v", names, wantNames)
			}
			if mode == "optional absent" && single != nil {
				t.Fatal("missing optional dependency was not nil")
			}
			if len(wantNames) == 1 && single.Name() != wantNames[0] {
				t.Fatalf("single service = %v", single)
			}
			wantSteps := map[string]string{
				"single":           "init one,publish one,bind consumer,init consumer",
				"selected":         "init two,publish two,bind consumer,init consumer,init one,publish one",
				"multiple":         "init one,publish one,init two,publish two,bind consumer,init consumer",
				"optional absent":  "bind consumer,init consumer",
				"optional present": "init one,publish one,bind consumer,init consumer",
			}[mode]
			if strings.Join(steps, ",") != wantSteps {
				t.Fatalf("steps = %v", steps)
			}
		})
	}
}

func TestInitializeRejectsInvalidPublications(t *testing.T) {
	var nilService *memoryService
	for _, test := range []struct {
		name   string
		values map[string]any
	}{
		{"missing", nil},
		{"extra", map[string]any{"storage": &memoryService{}, "extra": true}},
		{"wrong name", map[string]any{"other": &memoryService{}}},
		{"nil", map[string]any{"storage": nil}},
		{"typed nil", map[string]any{"storage": nilService}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := dependencyDescriptor("provider", &dependencyModule{provide: func() map[string]any { return test.values }})
			provider.Provides = []string{"storage"}
			consumer := dependencyDescriptor("consumer", &dependencyModule{bind: func(handlers.Dependencies) error { t.Fatal("bound invalid service"); return nil }})
			consumer.Requires = []handlers.Requirement{{Capability: "storage", Optional: true}}
			instances, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{consumer, provider}, nil)
			if err == nil || !strings.Contains(err.Error(), `module "provider"`) {
				t.Fatalf("error = %v", err)
			}
			if len(instances) != 1 || instances[0].Name != "provider" {
				t.Fatalf("initialized instances = %v", instances)
			}
		})
	}
}

func TestDependencyAccessRejectsWrongTypesAndCopiesSlices(t *testing.T) {
	provider := dependencyDescriptor("provider", &dependencyModule{provide: func() map[string]any { return map[string]any{"storage": &memoryService{name: "original"}} }})
	provider.Provides = []string{"storage"}
	consumer := dependencyDescriptor("consumer", &dependencyModule{bind: func(d handlers.Dependencies) error {
		if _, err := handlers.GetDependency[int](d, "storage"); err == nil {
			t.Error("single lookup accepted wrong type")
		}
		if values, err := handlers.GetDependencies[int](d, "storage"); err == nil || values != nil {
			t.Error("multiple lookup accepted wrong type")
		}
		if _, err := handlers.GetDependencies[any](d, "unknown"); err == nil {
			t.Error("multiple lookup accepted undeclared name")
		}
		values, err := handlers.GetDependencies[namedService](d, "storage")
		if err != nil {
			return err
		}
		values[0] = &memoryService{name: "replacement"}
		original, err := handlers.GetDependency[namedService](d, "storage")
		if err != nil {
			return err
		}
		if original.Name() != "original" {
			t.Error("returned slice changed dependency bindings")
		}
		return nil
	}})
	consumer.Requires = []handlers.Requirement{{Capability: "storage"}}
	if _, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{consumer, provider}, nil); err != nil {
		t.Fatal(err)
	}
}

// plainModule deliberately does not implement either dependency hook.
type plainModule struct{ initialized *bool }

func (m *plainModule) Init(context.Context, *server.ApplicationServer) error {
	*m.initialized = true
	return nil
}
func (*plainModule) Routes(*routing.Routes) error { return nil }

func TestInitializeChecksHooksBeforeInitialization(t *testing.T) {
	for _, hook := range []string{"DependencyBinder", "CapabilityProvider"} {
		t.Run(hook, func(t *testing.T) {
			initialized := false
			module := dependencyDescriptor("plain", &plainModule{initialized: &initialized})
			if hook == "DependencyBinder" {
				module.Requires = []handlers.Requirement{{Capability: "missing", Optional: true}}
			} else {
				module.Provides = []string{"storage"}
			}
			first := dependencyDescriptor("first", &plainModule{initialized: &initialized})
			instances, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{first, module}, nil)
			if err == nil || !strings.Contains(err.Error(), hook) || initialized || len(instances) != 0 {
				t.Fatalf("error = %v, initialized = %v, instances = %v", err, initialized, instances)
			}
		})
	}
}

func TestInitializeRejectsTypedNilBeforeHooks(t *testing.T) {
	for _, declaresCapabilities := range []bool{false, true} {
		t.Run(fmt.Sprintf("declaresCapabilities=%t", declaresCapabilities), func(t *testing.T) {
			catalog := []handlers.Descriptor{{Name: "nil-module", New: func() handlers.Handler { return (*dependencyModule)(nil) }}}
			if declaresCapabilities {
				catalog[0].Provides = []string{"storage"}
				catalog[0].Requires = []handlers.Requirement{{Capability: "optional", Optional: true}}
			}
			instances, err := handlers.Initialize(context.Background(), nil, catalog, nil)
			if len(instances) != 0 || err == nil || !strings.Contains(err.Error(), `module "nil-module" returned no instance`) {
				t.Fatalf("instances = %v, error = %v", instances, err)
			}
		})
	}
}

func TestInitializeRejectsInvalidCatalogAndMissingInstance(t *testing.T) {
	for _, invalidMetadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalidMetadata=%t", invalidMetadata), func(t *testing.T) {
			factoryCalled := false
			catalog := []handlers.Descriptor{{Name: "test", New: func() handlers.Handler {
				factoryCalled = true
				return nil
			}}}
			wantError := "returned no instance"
			if invalidMetadata {
				catalog[0].Requires = []handlers.Requirement{{Capability: "missing"}}
				wantError = "requires missing capability"
			}
			instances, err := handlers.Initialize(context.Background(), nil, catalog, nil)
			if err == nil || !strings.Contains(err.Error(), wantError) || len(instances) != 0 || factoryCalled == invalidMetadata {
				t.Fatalf("instances = %v, error = %v, factory called = %v", instances, err, factoryCalled)
			}
		})
	}
}

func TestInitializeStopsOnFailureAndCancellation(t *testing.T) {
	cause := errors.New("service unavailable")
	for _, phase := range []string{"before factories", "bind failure", "init failure", "cancel bind", "cancel init", "cancel publication"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var steps []string
			module := &dependencyModule{
				bind: func(handlers.Dependencies) error {
					steps = append(steps, "bind")
					if phase == "bind failure" {
						return cause
					}
					if phase == "cancel bind" {
						cancel()
					}
					return nil
				},
				init: func(got context.Context) error {
					steps = append(steps, "init")
					if got != ctx {
						t.Error("Init did not receive startup context")
					}
					if phase == "init failure" {
						return cause
					}
					if phase == "cancel init" {
						cancel()
					}
					return nil
				},
				provide: func() map[string]any {
					steps = append(steps, "publish")
					if phase == "cancel publication" {
						cancel()
					}
					return map[string]any{"storage": &memoryService{}}
				},
			}
			catalog := []handlers.Descriptor{{Name: "provider", Provides: []string{"storage"}, New: func() handlers.Handler { steps = append(steps, "factory"); return module }}}
			if phase == "before factories" {
				cancel()
			}
			instances, err := handlers.Initialize(ctx, nil, catalog, nil)
			wantErr := cause
			if strings.HasPrefix(phase, "cancel") || phase == "before factories" {
				wantErr = context.Canceled
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			wantSteps := map[string]string{"before factories": "", "bind failure": "factory,bind", "init failure": "factory,bind,init", "cancel bind": "factory,bind", "cancel init": "factory,bind,init", "cancel publication": "factory,bind,init,publish"}[phase]
			if strings.Join(steps, ",") != wantSteps {
				t.Fatalf("steps = %v", steps)
			}
			wantCount := 0
			if phase == "cancel init" || phase == "cancel publication" {
				wantCount = 1
			}
			if len(instances) != wantCount {
				t.Fatalf("initialized instances = %v", instances)
			}
		})
	}
}

func TestInitializeIsolatesApplications(t *testing.T) {
	catalog := []handlers.Descriptor{
		{Name: "consumer", Requires: []handlers.Requirement{{Capability: "storage"}}, New: func() handlers.Handler {
			return &dependencyModule{bind: func(d handlers.Dependencies) error {
				service, err := handlers.GetDependency[*memoryService](d, "storage")
				if err != nil {
					return err
				}
				if service.name != "fresh" {
					return fmt.Errorf("shared service state = %q", service.name)
				}
				service.name = "changed"
				return nil
			}}
		}},
		{Name: "provider", Provides: []string{"storage"}, New: func() handlers.Handler {
			service := &memoryService{name: "fresh"}
			return &dependencyModule{provide: func() map[string]any { return map[string]any{"storage": service} }}
		}},
	}
	for range 8 {
		t.Run("application", func(t *testing.T) {
			t.Parallel()
			if _, err := handlers.Initialize(context.Background(), nil, catalog, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInitializeOptionalProviderFailureStopsConsumer(t *testing.T) {
	cause := errors.New("storage unavailable")
	for _, phase := range []string{"init", "type"} {
		t.Run(phase, func(t *testing.T) {
			provider := dependencyDescriptor("provider", &dependencyModule{
				init: func(context.Context) error {
					if phase == "init" {
						return cause
					}
					return nil
				},
				provide: func() map[string]any { return map[string]any{"storage": "wrong service type"} },
			})
			provider.Provides = []string{"storage"}
			consumer := dependencyDescriptor("consumer", &dependencyModule{
				bind: func(d handlers.Dependencies) error {
					if phase == "init" {
						t.Fatal("bound consumer after provider failed")
					}
					_, err := handlers.GetDependency[namedService](d, "storage")
					return err
				},
				init: func(context.Context) error { t.Fatal("initialized consumer with broken optional provider"); return nil },
			})
			consumer.Requires = []handlers.Requirement{{Capability: "storage", Optional: true}}
			instances, err := handlers.Initialize(context.Background(), nil, []handlers.Descriptor{consumer, provider}, nil)
			if err == nil {
				t.Fatal("startup accepted broken optional provider")
			}
			if phase == "init" {
				if !errors.Is(err, cause) || len(instances) != 0 {
					t.Fatalf("instances = %v, error = %v", instances, err)
				}
			} else if !strings.Contains(err.Error(), `bind module "consumer"`) || len(instances) != 1 || instances[0].Name != "provider" {
				t.Fatalf("instances = %v, error = %v", instances, err)
			}
		})
	}
}

func TestGetDependenciesRejectsPartialResults(t *testing.T) {
	catalog := []handlers.Descriptor{}
	for _, name := range []string{"valid", "invalid"} {
		provider := dependencyDescriptor(name, &dependencyModule{provide: func() map[string]any {
			var value any = &memoryService{name: name}
			if name == "invalid" {
				value = "not a service"
			}
			return map[string]any{"storage": value}
		}})
		provider.Provides = []string{"storage"}
		catalog = append(catalog, provider)
	}
	consumer := dependencyDescriptor("consumer", &dependencyModule{bind: func(d handlers.Dependencies) error {
		values, err := handlers.GetDependencies[namedService](d, "storage")
		if err == nil || values != nil {
			t.Fatalf("values = %v, error = %v", values, err)
		}
		return err
	}})
	consumer.Requires = []handlers.Requirement{{Capability: "storage", Multiple: true}}
	catalog = append(catalog, consumer)
	instances, err := handlers.Initialize(context.Background(), nil, catalog, nil)
	if err == nil || len(instances) != 2 {
		t.Fatalf("instances = %v, error = %v", instances, err)
	}
}

func TestInitializePreservesCompletedInstancesOnLaterFailure(t *testing.T) {
	cause := errors.New("initialization failed")
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := dependencyDescriptor("first", &dependencyModule{provide: func() map[string]any {
				if canceled {
					cancel()
				}
				return map[string]any{"storage": &memoryService{}}
			}})
			first.Provides = []string{"storage"}
			second := dependencyDescriptor("second", &dependencyModule{bind: func(handlers.Dependencies) error {
				if canceled {
					t.Fatal("bound module after cancellation")
				}
				return nil
			}, init: func(context.Context) error { return cause }})
			last := dependencyDescriptor("last", &dependencyModule{bind: func(handlers.Dependencies) error { t.Fatal("bound module after failure"); return nil }})
			instances, err := handlers.Initialize(ctx, nil, []handlers.Descriptor{first, second, last}, nil)
			wantErr := cause
			if canceled {
				wantErr = context.Canceled
			}
			if !errors.Is(err, wantErr) || len(instances) != 1 || instances[0].Name != "first" {
				t.Fatalf("instances = %v, error = %v", instances, err)
			}
		})
	}
}
