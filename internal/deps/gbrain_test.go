package deps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedGBrainPathUsesGasTownCache(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	want := filepath.Join(cache, "gastown", "extensions", "gbrain", GBrainVersion, "node_modules", ".bin", "gbrain")
	if got := ManagedGBrainPath(); got != want {
		t.Fatalf("ManagedGBrainPath() = %q, want %q", got, want)
	}
}

func TestFindGBrainPrefersManagedExtension(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("PATH", t.TempDir())
	path := ManagedGBrainPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := FindGBrain(); got != path {
		t.Fatalf("FindGBrain() = %q, want %q", got, path)
	}
}

func TestEnsureManagedBunReplacesUnpinnedVersion(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	globalDir := t.TempDir()
	t.Setenv("PATH", globalDir)

	managed := filepath.Join(cache, "gastown", "extensions", "bun", "bin", "bun")
	if err := os.MkdirAll(filepath.Dir(managed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("#!/bin/sh\necho 0.0.1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(globalDir, "bun")
	if err := os.WriteFile(global, []byte("#!/bin/sh\necho "+BunVersion+"\n"), 0700); err != nil {
		t.Fatal(err)
	}

	path, err := ensureManagedBun()
	if err != nil {
		t.Fatal(err)
	}
	if path != managed {
		t.Fatalf("ensureManagedBun() = %q, want %q", path, managed)
	}
	version, err := bunVersion(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != BunVersion {
		t.Fatalf("managed Bun version = %q, want %q", version, BunVersion)
	}
}
