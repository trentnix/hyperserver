package handlers

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

type testModule struct{ app *server.ApplicationServer }

func (m *testModule) Init(_ context.Context, app *server.ApplicationServer) error {
	m.app = app
	return nil
}
func (*testModule) Routes(*routing.Routes) error { return nil }

func TestInstantiateIsolatedModules(t *testing.T) {
	catalog := []Descriptor{{Name: "test", New: func() Handler { return new(testModule) }}}
	var workers sync.WaitGroup
	instances := make(chan Handler, 20)
	for i := 0; i < cap(instances); i++ {
		workers.Go(func() {
			modules, err := Instantiate(catalog)
			if err != nil {
				t.Error(err)
				return
			}
			app := &server.ApplicationServer{}
			if err := modules[0].Init(context.Background(), app); err != nil {
				t.Error(err)
			}
			instances <- modules[0]
		})
	}
	workers.Wait()
	close(instances)
	seen := make(map[Handler]bool)
	for instance := range instances {
		if seen[instance] {
			t.Fatal("factory reused a runtime instance")
		}
		seen[instance] = true
	}
	if len(seen) != 20 {
		t.Fatal("not all applications received modules")
	}
	if modules, err := Instantiate(nil); err != nil || len(modules) != 0 {
		t.Fatal("empty local catalog used imports")
	}
}

func TestInvalidDescriptors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		descriptors []Descriptor
		want        string
	}{
		{"empty name", []Descriptor{{New: func() Handler { return new(testModule) }}}, "invalid"},
		{"missing factory", []Descriptor{{Name: "test"}}, "invalid"},
		{"duplicate", []Descriptor{{Name: "test", New: func() Handler { return new(testModule) }}, {Name: "test", New: func() Handler { return new(testModule) }}}, "duplicate"},
		{"nil instance", []Descriptor{{Name: "test", New: func() Handler { return nil }}}, "no instance"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if modules, err := Instantiate(tc.descriptors); modules != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("modules=%v error=%v", modules, err)
			}
		})
	}
	calls := 0
	_, err := Instantiate([]Descriptor{{Name: "first", New: func() Handler { calls++; return new(testModule) }}, {Name: "bad"}})
	if err == nil || calls != 0 {
		t.Fatal("factory ran before catalog validation")
	}
}

func TestRegisteredSnapshot(t *testing.T) {
	name := t.TempDir()
	Register(Descriptor{Name: name, New: func() Handler {
		t.Error("catalog access ran a factory")
		return new(testModule)
	}})
	snapshot := Registered()
	snapshot[len(snapshot)-1].Name = "changed"
	snapshot[len(snapshot)-1].New = nil
	if got := Registered(); got[len(got)-1].Name != name || got[len(got)-1].New == nil {
		t.Fatal("snapshot mutation changed the import catalog")
	}
}

func TestInstantiatePreservesCatalogOrder(t *testing.T) {
	var order []string
	catalog := []Descriptor{}
	for _, name := range []string{"second", "first"} {
		catalog = append(catalog, Descriptor{Name: name, New: func() Handler {
			order = append(order, name)
			return new(testModule)
		}})
	}
	if _, err := Instantiate(catalog); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "second,first" {
		t.Fatalf("factory order = %v", order)
	}
}

func TestReplace(t *testing.T) {
	original := &testModule{}
	replacement := &testModule{}
	catalog := []Descriptor{
		{Name: "first", New: func() Handler { return original }},
		{Name: "second", New: func() Handler { return new(testModule) }},
	}
	updated, err := Replace(catalog, Descriptor{Name: "first", New: func() Handler { return replacement }})
	if err != nil {
		t.Fatal(err)
	}
	if catalog[0].New() != original || updated[0].New() != replacement || updated[1].Name != "second" {
		t.Fatal("replacement changed the source or catalog order")
	}
	updated[1].Name = "changed"
	if catalog[1].Name != "second" {
		t.Fatal("replacement shares the source slice")
	}

	for _, tc := range []struct {
		name        string
		catalog     []Descriptor
		replacement Descriptor
		want        string
	}{
		{"unknown", catalog, Descriptor{Name: "missing", New: catalog[0].New}, "unknown module"},
		{"empty catalog", nil, catalog[0], "unknown module"},
		{"missing factory", catalog, Descriptor{Name: "first"}, "invalid module descriptor"},
		{"empty name", catalog, Descriptor{New: catalog[0].New}, "invalid module descriptor"},
		{"whitespace name", catalog, Descriptor{Name: " first ", New: catalog[0].New}, "invalid module descriptor"},
		{"duplicate", []Descriptor{catalog[0], catalog[0]}, catalog[0], "duplicate module"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Replace(tc.catalog, tc.replacement)
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("replacement = %v, error = %v", got, err)
			}
		})
	}
}
