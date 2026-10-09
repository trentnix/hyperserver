// Package handlers catalogs module factories. Imports register descriptions,
// and each application creates its own module instances before initialization.
package handlers

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/trentnix/hyperserver/pkg/server"
)

var catalog struct {
	sync.RWMutex
	descriptors []Descriptor
}

// Descriptor describes a module without holding its runtime state. New must return
// a fresh instance without performing I/O. Names must be nonempty and unique.
type Descriptor struct {
	Name string
	New  func() Handler
}

type (
	// Handler defines an interface that can be used to add routes and handlers to
	// a HyperServer application
	Handler interface {
		// Routes binds the initialized module's HTTP routes. Registration errors
		// must stop startup before accepting traffic.
		Routes(*routing.Routes) error
		// Init prepares the module before activation. Shared services are borrowed,
		// and the module must not close them. Use ctx for initialization I/O.
		Init(context.Context, *server.ApplicationServer) error
	}
)

// Register adds a descriptor to the import catalog. Invalid registrations are
// reported by Instantiate, not by a panic during package initialization.
func Register(d Descriptor) {
	catalog.Lock()
	defer catalog.Unlock()
	catalog.descriptors = append(catalog.descriptors, d)
}

// Registered returns a snapshot. Changing it does not change the import catalog.
func Registered() []Descriptor {
	catalog.RLock()
	defer catalog.RUnlock()
	return slices.Clone(catalog.descriptors)
}

// Replace returns a catalog copy with one named module's factory replaced.
// The name must occur exactly once and the replacement must have a factory.
// Neither the supplied catalog nor the process-wide catalog is changed.
func Replace(descriptors []Descriptor, replacement Descriptor) ([]Descriptor, error) {
	if replacement.Name == "" || strings.TrimSpace(replacement.Name) != replacement.Name || replacement.New == nil {
		return nil, fmt.Errorf("invalid module descriptor %q", replacement.Name)
	}
	index := -1
	for i, d := range descriptors {
		if d.Name != replacement.Name {
			continue
		}
		if index >= 0 {
			return nil, fmt.Errorf("duplicate module %q", replacement.Name)
		}
		index = i
	}
	if index < 0 {
		return nil, fmt.Errorf("unknown module %q", replacement.Name)
	}
	result := slices.Clone(descriptors)
	result[index] = replacement
	return result, nil
}

// Instantiate validates a catalog and creates fresh modules in registration order.
// Tests and applications can supply a local catalog, including an empty one.
func Instantiate(descriptors []Descriptor) ([]Handler, error) {
	seen := make(map[string]bool)
	for _, d := range descriptors {
		if d.Name == "" || strings.TrimSpace(d.Name) != d.Name || d.New == nil {
			return nil, fmt.Errorf("invalid module descriptor %q", d.Name)
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("duplicate module %q", d.Name)
		}
		seen[d.Name] = true
	}

	instances := make([]Handler, 0, len(descriptors))
	for _, d := range descriptors {
		h := d.New()
		if h == nil {
			return nil, fmt.Errorf("module %q returned no instance", d.Name)
		}
		instances = append(instances, h)
	}
	return instances, nil
}
