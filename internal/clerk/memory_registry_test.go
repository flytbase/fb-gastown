package clerk

import (
	"path/filepath"
	"testing"
)

func TestMemoryRegistryRetainsRelocatedRoots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	first := MemoryPolicy{ID: "clerk-town", Root: filepath.Join(t.TempDir(), "first")}
	second := MemoryPolicy{ID: "clerk-town", Root: filepath.Join(t.TempDir(), "second")}

	if err := RegisterMemoryPolicy("/town", first); err != nil {
		t.Fatal(err)
	}
	if err := RegisterMemoryPolicy("/town", second); err != nil {
		t.Fatal(err)
	}
	entries, err := RegisteredMemoryPolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("registered roots = %d, want 2", len(entries))
	}

	if err := UnregisterMemoryRoot(first.Root); err != nil {
		t.Fatal(err)
	}
	entries, err = RegisteredMemoryPolicies()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !sameMemoryRoot(entries[0].Root, second.Root) {
		t.Fatalf("remaining registry entries = %#v, want only second root", entries)
	}
}

func sameMemoryRoot(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}
