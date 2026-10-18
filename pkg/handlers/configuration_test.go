package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type configurationModule struct {
	settings struct {
		Title string `yaml:"title"`
	}
	steps         *[]string
	configuration func() any
	titleOverride string
}

func (m *configurationModule) Configuration() any {
	*m.steps = append(*m.steps, "configuration")
	if m.configuration != nil {
		return m.configuration()
	}
	return &m.settings
}
func (m *configurationModule) BindDependencies(handlers.Dependencies) error {
	*m.steps = append(*m.steps, "bind "+m.settings.Title)
	return nil
}
func (m *configurationModule) Init(context.Context, *server.ApplicationServer) error {
	if m.titleOverride != "" {
		m.settings.Title = m.titleOverride
	}
	*m.steps = append(*m.steps, "init "+m.settings.Title)
	if m.settings.Title == "" {
		return errors.New("title required")
	}
	return nil
}
func (m *configurationModule) Routes(*routing.Routes) error {
	*m.steps = append(*m.steps, "routes")
	return nil
}
func (m *configurationModule) Shutdown(context.Context) error {
	*m.steps = append(*m.steps, "shutdown")
	return nil
}

func configurationDescriptor(steps *[]string) handlers.Descriptor {
	return handlers.Descriptor{Name: "configured", New: func() handlers.Handler {
		m := &configurationModule{steps: steps}
		m.settings.Title = "Default"
		return m
	}}
}

func configurationApplication(t *testing.T) *server.ApplicationServer {
	t.Helper()
	app, err := server.NewApplicationServer(config.Config{App: config.AppConfig{WorkingDirectory: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	return app
}

func TestModuleConfigurationLifecycle(t *testing.T) {
	for _, mode := range []string{"file", "missing", "missing directory", "empty", "unconfigured", "override"} {
		t.Run(mode, func(t *testing.T) {
			app := configurationApplication(t)
			var steps []string
			d := configurationDescriptor(&steps)
			want := []string{"configuration", "bind Default", "init Default", "routes"}
			path := app.Config.FilePath(filepath.Join("modules", d.Name, "config.yaml"))
			if mode != "missing directory" {
				path = configurationPath(t, app, d.Name)
			}
			switch mode {
			case "file", "override":
				if err := os.WriteFile(path, []byte("title: Loaded"), 0600); err != nil {
					t.Fatal(err)
				}
				want = []string{"configuration", "bind Loaded", "init Loaded", "routes"}
				if mode == "override" {
					factory := d.New
					d.New = func() handlers.Handler {
						m := factory().(*configurationModule)
						m.titleOverride = "Explicit"
						return m
					}
					want[2] = "init Explicit"
				}
			case "empty":
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "unconfigured":
				d = handlers.Descriptor{Name: "plain", New: func() handlers.Handler { return &plainConfigurationModule{steps: &steps} }}
				// Modules without the hook do not read even an invalid file.
				if err := os.WriteFile(configurationPath(t, app, d.Name), []byte("invalid: ["), 0600); err != nil {
					t.Fatal(err)
				}
				want = []string{"plain init", "plain routes"}
			}
			if err := handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{d}, nil); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("steps=%v, want %v", steps, want)
			}
		})
	}
}

func configurationPath(t *testing.T, app *server.ApplicationServer, name string) string {
	t.Helper()
	path := app.Config.FilePath(filepath.Join("modules", name, "config.yaml"))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

type plainConfigurationModule struct{ steps *[]string }

func (m *plainConfigurationModule) Init(context.Context, *server.ApplicationServer) error {
	*m.steps = append(*m.steps, "plain init")
	return nil
}
func (m *plainConfigurationModule) Routes(*routing.Routes) error {
	*m.steps = append(*m.steps, "plain routes")
	return nil
}

func TestModuleConfigurationFailuresRollback(t *testing.T) {
	for _, mode := range []string{"syntax", "unknown", "validation", "missing required", "unreadable", "nil settings"} {
		t.Run(mode, func(t *testing.T) {
			app := configurationApplication(t)
			var firstSteps, failedSteps []string
			first, failed := configurationDescriptor(&firstSteps), configurationDescriptor(&failedSteps)
			first.Name = "first"
			path := configurationPath(t, app, failed.Name)
			input := "title: ["
			switch mode {
			case "unknown":
				input = "typo: value"
			case "validation":
				input = "title: ''"
			case "missing required":
				failed.New = func() handlers.Handler { return &configurationModule{steps: &failedSteps} }
			case "unreadable":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "nil settings":
				failed.New = func() handlers.Handler {
					return &configurationModule{steps: &failedSteps, configuration: func() any { return nil }}
				}
			}
			if mode != "unreadable" && mode != "missing required" {
				if err := os.WriteFile(path, []byte(input), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{first, failed}, nil)
			if err == nil || !strings.Contains(err.Error(), `module "configured"`) {
				t.Fatalf("missing module error: %v", err)
			}
			if !reflect.DeepEqual(firstSteps, []string{"configuration", "bind Default", "init Default", "shutdown"}) {
				t.Fatalf("initialized module not rolled back: %v", firstSteps)
			}
			if mode != "validation" && mode != "missing required" && !reflect.DeepEqual(failedSteps, []string{"configuration"}) {
				t.Fatalf("failed configuration reached initialization: %v", failedSteps)
			}
			for _, step := range failedSteps {
				if step == "routes" || step == "shutdown" {
					t.Fatalf("uninitialized module used: %v", failedSteps)
				}
			}
		})
	}
}

func TestModuleConfigurationPreflight(t *testing.T) {
	var steps []string
	d := configurationDescriptor(&steps)
	for _, app := range []*server.ApplicationServer{nil, {}} {
		if _, err := handlers.Initialize(t.Context(), app, []handlers.Descriptor{d}, nil); err == nil || len(steps) != 0 {
			t.Fatalf("missing application settings: %v, %v", steps, err)
		}
	}
}

func TestModuleConfigurationRejectsPathNames(t *testing.T) {
	app := configurationApplication(t)
	var steps []string
	d := configurationDescriptor(&steps)
	for _, name := range []string{".", "..", "../outside", "nested/name", `nested\name`, "/absolute"} {
		d.Name = name
		if _, err := handlers.Initialize(t.Context(), app, []handlers.Descriptor{d}, nil); err == nil || !strings.Contains(err.Error(), "directory name") || len(steps) != 0 {
			t.Fatalf("invalid configuration directory %q: %v, %v", name, steps, err)
		}
	}
}

func TestConfigurationUsesRegisteredName(t *testing.T) {
	app := configurationApplication(t)
	var steps []string
	first, second := configurationDescriptor(&steps), configurationDescriptor(&steps)
	first.Name, second.Name = "first", "second"
	for _, d := range []handlers.Descriptor{first, second} {
		if err := os.WriteFile(configurationPath(t, app, d.Name), []byte("title: "+d.Name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := handlers.Setup(t.Context(), app, nil, []handlers.Descriptor{first, second}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"configuration", "bind first", "init first", "configuration", "bind second", "init second", "routes", "routes"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("configuration did not follow registered names: %v", steps)
	}
}

func TestModuleConfigurationIsolation(t *testing.T) {
	var steps []string
	d := configurationDescriptor(&steps)
	var instances []*configurationModule
	for _, title := range []string{"First", "Second"} {
		app := configurationApplication(t)
		if err := os.WriteFile(configurationPath(t, app, d.Name), []byte("title: "+title), 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := handlers.Initialize(t.Context(), app, []handlers.Descriptor{d}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := handlers.Close(context.WithoutCancel(t.Context()), loaded); err != nil {
				t.Error(err)
			}
		})
		instances = append(instances, loaded[0].Handler.(*configurationModule))
		if err := os.WriteFile(configurationPath(t, app, d.Name), []byte("title: Changed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if instances[0] == instances[1] || instances[0].settings.Title != "First" || instances[1].settings.Title != "Second" {
		t.Fatal("catalog shared module configuration")
	}
}

func TestModuleConfigurationCancellation(t *testing.T) {
	app := configurationApplication(t)
	if err := os.WriteFile(configurationPath(t, app, "canceled"), []byte("broken: ["), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var steps []string
	m := &configurationModule{steps: &steps}
	m.configuration = func() any { cancel(); return &m.settings }
	d := handlers.Descriptor{Name: "canceled", New: func() handlers.Handler { return m }}
	var firstSteps []string
	first := configurationDescriptor(&firstSteps)
	instances, err := handlers.Initialize(ctx, app, []handlers.Descriptor{first, d}, nil)
	if !errors.Is(err, context.Canceled) || len(instances) != 0 || !reflect.DeepEqual(steps, []string{"configuration"}) {
		t.Fatalf("canceled setup continued: %v, %v", steps, err)
	}
	if !reflect.DeepEqual(firstSteps, []string{"configuration", "bind Default", "init Default", "shutdown"}) {
		t.Fatalf("canceled configuration did not roll back initialized modules: %v", firstSteps)
	}
}
