package handlers

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

func resolutionModule(name string, provides []string, requires ...Requirement) Descriptor {
	return Descriptor{Name: name, New: func() Handler { return new(testModule) }, Provides: provides, Requires: requires}
}

func TestResolve(t *testing.T) {
	consumer := resolutionModule("pages", nil, Requirement{Capability: "storage"})
	disk := resolutionModule("disk", []string{"storage"})
	cloud := resolutionModule("cloud", []string{"storage"})
	for _, tc := range []struct {
		name       string
		catalog    []Descriptor
		selections map[string]string
		order      string
		wantError  string
	}{
		{name: "empty"},
		{name: "independent modules keep order", catalog: []Descriptor{disk, cloud}, order: "disk,cloud"},
		{name: "provider before consumer", catalog: []Descriptor{consumer, disk}, order: "disk,pages"},
		{name: "explicit provider", catalog: []Descriptor{consumer, disk, cloud}, selections: map[string]string{"storage": "cloud"}, order: "cloud,pages,disk"},
		{name: "missing provider", catalog: []Descriptor{consumer}, wantError: `module "pages" requires missing capability "storage"`},
		{name: "ambiguous provider", catalog: []Descriptor{consumer, disk, cloud}, wantError: "select one provider from disk, cloud"},
		{name: "unknown selected module", catalog: []Descriptor{consumer, disk}, selections: map[string]string{"storage": "typo"}, wantError: `selected module "typo" does not provide it`},
		{name: "wrong capability", catalog: []Descriptor{consumer, disk}, selections: map[string]string{"storage": "pages"}, wantError: `selected module "pages" does not provide it`},
		{name: "unknown selected capability", catalog: []Descriptor{disk}, selections: map[string]string{"typo": "disk"}, wantError: `capability "typo"`},
		{name: "invalid unused selection", catalog: []Descriptor{disk}, selections: map[string]string{"storage": "missing"}, wantError: `selected module "missing"`},
		{name: "selection errors are deterministic", catalog: []Descriptor{disk}, selections: map[string]string{"z-last": "disk", "a-first": "disk"}, wantError: `capability "a-first"`},
		{name: "duplicate module", catalog: []Descriptor{disk, disk}, wantError: `duplicate module "disk"`},
		{name: "self dependency", catalog: []Descriptor{resolutionModule("self", []string{"storage"}, Requirement{Capability: "storage"})}, wantError: "self -> self"},
		{name: "cycle", catalog: []Descriptor{
			resolutionModule("a", []string{"a"}, Requirement{Capability: "b"}),
			resolutionModule("b", []string{"b"}, Requirement{Capability: "c"}),
			resolutionModule("c", []string{"c"}, Requirement{Capability: "a"}),
		}, wantError: "a -> b -> c -> a"},
		{name: "diamond", catalog: []Descriptor{
			resolutionModule("top", nil, Requirement{Capability: "left"}, Requirement{Capability: "right"}),
			resolutionModule("left", []string{"left"}, Requirement{Capability: "base"}),
			resolutionModule("right", []string{"right"}, Requirement{Capability: "base"}),
			resolutionModule("base", []string{"base"}),
		}, order: "base,left,right,top"},
		{name: "one provider satisfies several requirements", catalog: []Descriptor{
			resolutionModule("pages", nil, Requirement{Capability: "read"}, Requirement{Capability: "write"}),
			resolutionModule("storage", []string{"read", "write"}),
		}, order: "storage,pages"},
		{name: "explicit selection narrows multiple providers", catalog: []Descriptor{
			resolutionModule("pages", []string{"pages"}, Requirement{Capability: "storage", Multiple: true}),
			resolutionModule("one", []string{"storage"}, Requirement{Capability: "pages"}),
			resolutionModule("two", []string{"storage"}),
		}, selections: map[string]string{"storage": "two"}, order: "two,pages,one"},
		{name: "unchosen providers remain validated", catalog: []Descriptor{
			consumer, disk,
			resolutionModule("other", []string{"other"}, Requirement{Capability: "other"}),
		}, selections: map[string]string{"storage": "disk"}, wantError: "other -> other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.catalog {
				tc.catalog[i].New = func() Handler { t.Error("resolution ran a factory"); return nil }
			}
			got, err := Resolve(tc.catalog, tc.selections)
			if tc.wantError != "" {
				if got != nil || err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("catalog=%v, error=%v, want %q", got, err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, d := range got {
				names = append(names, d.Name)
			}
			if strings.Join(names, ",") != tc.order {
				t.Fatalf("order=%v, want %s", names, tc.order)
			}
		})
	}
}

func TestResolveOptionalAndMultiple(t *testing.T) {
	for _, optional := range []bool{false, true} {
		for _, multiple := range []bool{false, true} {
			for count := 0; count <= 2; count++ {
				catalog := []Descriptor{resolutionModule("consumer", nil, Requirement{Capability: "storage", Optional: optional, Multiple: multiple})}
				for _, name := range []string{"one", "two"}[:count] {
					catalog = append(catalog, resolutionModule(name, []string{"storage"}))
				}
				got, err := Resolve(catalog, nil)
				wantError := count == 0 && !optional || count > 1 && !multiple
				if (err != nil) != wantError {
					t.Fatalf("optional=%t multiple=%t providers=%d: %v", optional, multiple, count, err)
				}
				if !wantError && (len(got) != count+1 || got[len(got)-1].Name != "consumer") {
					t.Fatalf("providers did not precede consumer: %v", got)
				}
				if count > 0 {
					if _, err := Resolve(catalog, map[string]string{"storage": "one"}); err != nil {
						t.Fatalf("explicit selection: %v", err)
					}
				}
			}
		}
	}
}

func TestResolveRejectsInvalidMetadata(t *testing.T) {
	for _, d := range []Descriptor{
		{Name: "missing-factory"},
		resolutionModule("", nil),
		resolutionModule(" space ", nil),
		resolutionModule("test", []string{""}),
		resolutionModule("test", []string{" storage "}),
		resolutionModule("test", []string{"storage", "storage"}),
		resolutionModule("test", nil, Requirement{}),
		resolutionModule("test", nil, Requirement{Capability: " storage "}),
		resolutionModule("test", nil, Requirement{Capability: "storage"}, Requirement{Capability: "storage", Optional: true}),
	} {
		if got, err := Resolve([]Descriptor{d}, nil); got != nil || err == nil {
			t.Fatalf("invalid descriptor accepted: %+v", d)
		}
	}
}

func TestResolveConcurrentSnapshots(t *testing.T) {
	catalog := []Descriptor{
		resolutionModule("consumer", nil, Requirement{Capability: "storage"}),
		resolutionModule("one", []string{"storage"}),
		resolutionModule("two", []string{"storage"}),
	}
	var workers sync.WaitGroup
	for _, choice := range []string{"one", "two", "one", "two"} {
		workers.Go(func() {
			for attempt := 0; attempt < 20; attempt++ {
				got, err := Resolve(catalog, map[string]string{"storage": choice})
				if err != nil {
					t.Error(err)
					return
				}
				if got[0].Name != choice || got[1].Name != "consumer" {
					t.Errorf("selection changed: %v", got)
				}
				got[0].Provides[0] = "changed"
				got[1].Requires[0].Capability = "changed"
			}
		})
	}
	workers.Wait()
	if catalog[0].Requires[0].Capability != "storage" || !slices.Equal(catalog[1].Provides, []string{"storage"}) {
		t.Fatal("resolution changed source metadata")
	}
}

func TestCatalogCopiesCapabilityMetadata(t *testing.T) {
	d := resolutionModule(t.TempDir(), []string{"storage"}, Requirement{Capability: "logging", Optional: true})
	Register(d)
	d.Provides[0] = "changed"
	d.Requires[0].Capability = "changed"
	snapshot := Registered()
	last := len(snapshot) - 1
	if snapshot[last].Provides[0] != "storage" || snapshot[last].Requires[0].Capability != "logging" {
		t.Fatal("registration kept caller-owned metadata")
	}
	snapshot[last].Provides[0] = "changed"
	snapshot[last].Requires[0].Capability = "changed"
	if got := Registered(); got[last].Provides[0] != "storage" || got[last].Requires[0].Capability != "logging" {
		t.Fatal("snapshot exposed global metadata")
	}

	original := resolutionModule("test", []string{"storage"}, Requirement{Capability: "logging", Optional: true})
	replacement := resolutionModule("test", []string{"files"}, Requirement{Capability: "logging", Optional: true})
	got, err := Replace([]Descriptor{original}, replacement)
	if err != nil {
		t.Fatal(err)
	}
	got[0].Provides[0] = "changed"
	got[0].Requires[0].Capability = "changed"
	if replacement.Provides[0] != "files" || replacement.Requires[0].Capability != "logging" {
		t.Fatal("replacement exposed caller metadata")
	}
}
