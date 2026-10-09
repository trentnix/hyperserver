package handlers

import (
	"fmt"
	"slices"
	"strings"
)

// Resolve validates the selected modules and returns a catalog copy with providers
// before their consumers. It performs no I/O and never invokes factories.
// selections maps capability names to provider module names. Without a selection,
// a requirement must have exactly one provider unless it permits zero or multiple.
// All supplied modules remain selected, including unchosen alternative providers.
// Resolution orders initialization. It does not inject runtime dependencies.
func Resolve(descriptors []Descriptor, selections map[string]string) ([]Descriptor, error) {
	if err := validateDescriptors(descriptors); err != nil {
		return nil, err
	}
	providers := make(map[string][]int)
	for i, d := range descriptors {
		for _, capability := range d.Provides {
			providers[capability] = append(providers[capability], i)
		}
	}

	// Check every explicit selection, including currently unused capabilities.
	// Sorting makes configuration errors independent of map iteration order.
	capabilities := make([]string, 0, len(selections))
	for capability := range selections {
		capabilities = append(capabilities, capability)
	}
	slices.Sort(capabilities)
	for _, capability := range capabilities {
		chosen := -1
		for _, i := range providers[capability] {
			if descriptors[i].Name == selections[capability] {
				chosen = i
				break
			}
		}
		if chosen < 0 {
			return nil, fmt.Errorf("capability %q: selected module %q does not provide it", capability, selections[capability])
		}
		providers[capability] = []int{chosen}
	}

	dependencies := make([][]int, len(descriptors))
	for i, d := range descriptors {
		for _, requirement := range d.Requires {
			matches := providers[requirement.Capability]
			if len(matches) == 0 && !requirement.Optional {
				return nil, fmt.Errorf("module %q requires missing capability %q", d.Name, requirement.Capability)
			}
			if len(matches) > 1 && !requirement.Multiple {
				names := make([]string, len(matches))
				for j, provider := range matches {
					names[j] = descriptors[provider].Name
				}
				return nil, fmt.Errorf("module %q requires capability %q: select one provider from %s", d.Name, requirement.Capability, strings.Join(names, ", "))
			}
			for _, provider := range matches {
				if !slices.Contains(dependencies[i], provider) {
					dependencies[i] = append(dependencies[i], provider)
				}
			}
		}
	}

	var ordered []Descriptor
	var path []string
	const (
		visiting = 1
		visited  = 2
	)
	state := make([]uint8, len(descriptors))
	var visit func(int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			start := slices.Index(path, descriptors[i].Name)
			cycle := append(slices.Clone(path[start:]), descriptors[i].Name)
			return fmt.Errorf("module dependency cycle: %s", strings.Join(cycle, " -> "))
		case visited:
			return nil
		}
		state[i] = visiting
		path = append(path, descriptors[i].Name)
		for _, dependency := range dependencies[i] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[i] = visited
		ordered = append(ordered, cloneDescriptor(descriptors[i]))
		return nil
	}
	for i := range descriptors {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func validateDescriptors(descriptors []Descriptor) error {
	names := make(map[string]bool)
	for _, d := range descriptors {
		if !validName(d.Name) || d.New == nil {
			return fmt.Errorf("invalid module descriptor %q", d.Name)
		}
		if names[d.Name] {
			return fmt.Errorf("duplicate module %q", d.Name)
		}
		names[d.Name] = true
		provided := make(map[string]bool)
		for _, capability := range d.Provides {
			if !validName(capability) || provided[capability] {
				return fmt.Errorf("module %q: invalid or duplicate provided capability %q", d.Name, capability)
			}
			provided[capability] = true
		}
		required := make(map[string]bool)
		for _, requirement := range d.Requires {
			if !validName(requirement.Capability) || required[requirement.Capability] {
				return fmt.Errorf("module %q: invalid or duplicate required capability %q", d.Name, requirement.Capability)
			}
			required[requirement.Capability] = true
		}
	}
	return nil
}

func validName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name
}
