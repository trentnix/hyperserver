package handlers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/trentnix/hyperserver/pkg/server"
)

// CapabilityProvider exposes the services named by its descriptor's Provides list.
// Call after Init succeeds. Capabilities must perform no I/O and must return exactly
// the declared, non-nil services. Consumers borrow the services and must not close them.
type CapabilityProvider interface {
	Capabilities() map[string]any
}

// DependencyBinder accepts selected, initialized services before its Init runs.
// Modules with Requires must implement it. BindDependencies must only wire and
// validate dependencies, not perform I/O. Return errors for incompatible services.
type DependencyBinder interface {
	BindDependencies(Dependencies) error
}

// Dependencies contains only the capabilities declared by one module. Optional
// capabilities without a provider have no values. Use GetDependency or GetDependencies
// to read typed services. No application-wide lookup or mutation is exposed.
type Dependencies struct{ values map[string][]any }

// GetDependency returns one declared service as T. An absent optional capability
// returns T's zero value and no error. Undeclared names, multiple providers, and
// incompatible types return errors. Prefer a consumer-owned interface for T.
func GetDependency[T any](dependencies Dependencies, capability string) (T, error) {
	var zero T
	values, ok := dependencies.values[capability]
	if !ok {
		return zero, fmt.Errorf("undeclared capability %q", capability)
	}
	if len(values) == 0 {
		return zero, nil
	}
	if len(values) != 1 {
		return zero, fmt.Errorf("capability %q has multiple providers: use GetDependencies", capability)
	}
	value, ok := values[0].(T)
	if !ok {
		return zero, fmt.Errorf("capability %q: provider type %T does not satisfy %v", capability, values[0], reflect.TypeFor[T]())
	}
	return value, nil
}

// GetDependencies returns a new slice of declared services in catalog order.
// An absent optional capability returns an empty slice. Undeclared names and
// incompatible types return errors rather than partial results.
func GetDependencies[T any](dependencies Dependencies, capability string) ([]T, error) {
	values, ok := dependencies.values[capability]
	if !ok {
		return nil, fmt.Errorf("undeclared capability %q", capability)
	}
	var result []T
	for _, value := range values {
		service, ok := value.(T)
		if !ok {
			return nil, fmt.Errorf("capability %q: provider type %T does not satisfy %v", capability, value, reflect.TypeFor[T]())
		}
		result = append(result, service)
	}
	return result, nil
}

// Instance is an initialized, application-owned module. The returned order places
// providers before consumers. Bind routes only after all initialization succeeds.
type Instance struct {
	Name string
	Handler
}

// Initialize resolves requirements, creates modules, binds declared dependencies,
// and initializes modules in dependency order. It checks cancellation between steps.
// It does not bind routes or close resources. Applications still own shared services
// and failure cleanup. Modules must clean up resources they acquire on failed Init.
// On failure, the returned instances contain every module whose Init succeeded,
// including a module that subsequently failed to publish its capabilities.
func Initialize(ctx context.Context, app *server.ApplicationServer, catalog []Descriptor, selections map[string]string) ([]Instance, error) {
	plan, err := resolve(catalog, selections)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modules, err := Instantiate(plan.modules)
	if err != nil {
		return nil, err
	}
	// Detect missing hooks before any module starts I/O.
	for i, d := range plan.modules {
		if _, ok := modules[i].(DependencyBinder); len(d.Requires) > 0 && !ok {
			return nil, fmt.Errorf("module %q declares requirements but does not implement DependencyBinder", d.Name)
		}
		if _, ok := modules[i].(CapabilityProvider); len(d.Provides) > 0 && !ok {
			return nil, fmt.Errorf("module %q declares capabilities but does not implement CapabilityProvider", d.Name)
		}
	}

	result := make([]Instance, 0, len(modules))
	published := make(map[string]map[string]any)
	for i, module := range modules {
		d := plan.modules[i]
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("initialize module %q: %w", d.Name, err)
		}
		dependencies := Dependencies{values: make(map[string][]any)}
		for _, requirement := range d.Requires {
			dependencies.values[requirement.Capability] = nil
			for _, provider := range plan.bindings[d.Name][requirement.Capability] {
				dependencies.values[requirement.Capability] = append(dependencies.values[requirement.Capability], published[provider][requirement.Capability])
			}
		}
		if binder, ok := module.(DependencyBinder); ok {
			if err := binder.BindDependencies(dependencies); err != nil {
				return result, fmt.Errorf("bind module %q dependencies: %w", d.Name, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("initialize module %q: %w", d.Name, err)
		}
		if err := module.Init(ctx, app); err != nil {
			return result, fmt.Errorf("initialize module %q: %w", d.Name, err)
		}
		result = append(result, Instance{Name: d.Name, Handler: module})
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("initialize module %q: %w", d.Name, err)
		}
		if len(d.Provides) > 0 {
			values := module.(CapabilityProvider).Capabilities()
			if len(values) != len(d.Provides) {
				return result, fmt.Errorf("module %q must publish exactly its declared capabilities", d.Name)
			}
			published[d.Name] = make(map[string]any, len(values))
			for _, capability := range d.Provides {
				value := values[capability]
				if isNil(value) {
					return result, fmt.Errorf("module %q returned no service for capability %q", d.Name, capability)
				}
				published[d.Name][capability] = value
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
