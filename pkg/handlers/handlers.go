// Package handlers catalogs module factories. Imports register descriptions,
// and each application creates its own module instances before initialization.
package handlers

import (
	"context"
	"fmt"
	"reflect"
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
	Name     string
	New      func() Handler
	Provides []string
	Requires []Requirement
}

// Requirement names a capability needed before the consuming module initializes.
// Optional permits no provider. Multiple permits all matching providers instead
// of requiring a single provider. An explicit selection narrows either mode.
type Requirement struct {
	Capability string
	Optional   bool
	Multiple   bool
}

type (
	// Handler defines an interface that can be used to add routes and handlers to
	// a HyperServer application
	Handler interface {
		// Routes binds the module's own routes and those of its internal components.
		// Call after Init succeeds. Registration errors
		// must stop startup before accepting traffic.
		Routes(*routing.Routes) error
		// Init validates the module's requirements and prepares its internal components.
		// Applications supply shared services. Modules must not initialize or close
		// borrowed services. Use ctx for initialization I/O.
		Init(context.Context, *server.ApplicationServer) error
	}
)

// Register adds a descriptor to the import catalog. Invalid registrations are
// reported by Instantiate, not by a panic during package initialization.
func Register(d Descriptor) {
	catalog.Lock()
	defer catalog.Unlock()
	catalog.descriptors = append(catalog.descriptors, cloneDescriptor(d))
}

// Registered returns a snapshot. Changing it does not change the import catalog.
func Registered() []Descriptor {
	catalog.RLock()
	defer catalog.RUnlock()
	return cloneDescriptors(catalog.descriptors)
}

// Replace returns a catalog copy with one named module's entire descriptor replaced,
// including Provides and Requires. Use the module's descriptor constructor to retain
// its contracts when configuring it. To change only New, copy the original descriptor.
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
	result := cloneDescriptors(descriptors)
	result[index] = cloneDescriptor(replacement)
	return result, nil
}

// Instantiate validates a catalog and creates fresh modules in registration order.
// Tests and applications can supply a local catalog, including an empty one.
// Call Resolve first when modules declare capability requirements.
func Instantiate(descriptors []Descriptor) ([]Handler, error) {
	if err := validateDescriptors(descriptors); err != nil {
		return nil, err
	}

	instances := make([]Handler, 0, len(descriptors))
	for _, d := range descriptors {
		h := d.New()
		if isNil(h) {
			return nil, fmt.Errorf("module %q returned no instance", d.Name)
		}
		instances = append(instances, h)
	}
	return instances, nil
}

// isNil also recognizes a nil pointer stored inside an interface.
func isNil(value any) bool {
	if value == nil {
		return true
	}
	switch v := reflect.ValueOf(value); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func cloneDescriptor(d Descriptor) Descriptor {
	d.Provides = slices.Clone(d.Provides)
	d.Requires = slices.Clone(d.Requires)
	return d
}

func cloneDescriptors(descriptors []Descriptor) []Descriptor {
	result := slices.Clone(descriptors)
	for i := range result {
		result[i] = cloneDescriptor(result[i])
	}
	return result
}
