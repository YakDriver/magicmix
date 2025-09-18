package strategy

import (
	"fmt"
	"sort"
)

// Factory constructs a new sorter instance.
type Factory func() Sorter

var (
	factories = map[string]Factory{
		defaultStrategyName: func() Sorter { return NewDefaultSorter() },
	}
)

// Register adds or replaces a sorter factory in the registry.
func Register(name string, factory Factory) {
	factories[name] = factory
}

// Get returns a sorter by name.
func Get(name string) (Sorter, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown strategy: %s", name)
	}
	return factory(), nil
}

// Names returns a sorted list of registered strategy names for help output.
func Names() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
