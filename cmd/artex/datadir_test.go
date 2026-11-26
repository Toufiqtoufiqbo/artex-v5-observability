package main

import (
	"path/filepath"
	"testing"
)

func TestValidateDataDir(t *testing.T) {
	base := t.TempDir()
	if err := validateDataDir(filepath.Join(base, "data"), base); err != nil {
		t.Fatalf("default nested data dir should be allowed: %v", err)
	}
	if err := validateDataDir(base, base); err == nil {
		t.Fatal("data dir equal to the config base dir must be refused")
	}
	if err := validateDataDir(t.TempDir(), base); err != nil {
		t.Fatalf("unrelated data dir should be allowed: %v", err)
	}
}
