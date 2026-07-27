// Package specialist defines native Clerk-supervised Polecat specializations.
package specialist

import (
	"fmt"
	"sort"
	"strings"
)

// Definition contains the transport-neutral policy for a specialist kind.
// Gas Town still carries specialists through the native Polecat lifecycle.
type Definition struct {
	Kind        string
	DisplayName string
	Summary     string
	ReportOnly  bool
}

var native = map[string]Definition{
	"scrutineer": {
		Kind:        "scrutineer",
		DisplayName: "Scrutineer",
		Summary:     "reviews proposed changes and reports evidence-backed findings",
		ReportOnly:  true,
	},
	"surveyor": {
		Kind:        "surveyor",
		DisplayName: "Surveyor",
		Summary:     "maps a task, its dependencies, and risks into an execution plan",
		ReportOnly:  true,
	},
}

// Resolve returns the native definition for kind.
func Resolve(kind string) (Definition, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	definition, ok := native[normalized]
	if !ok {
		return Definition{}, fmt.Errorf("unknown specialist kind %q", kind)
	}
	return definition, nil
}

// List returns all native specialist definitions in stable kind order.
func List() []Definition {
	definitions := make([]Definition, 0, len(native))
	for _, definition := range native {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Kind < definitions[j].Kind
	})
	return definitions
}
