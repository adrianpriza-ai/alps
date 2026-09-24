package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
)

func TestIsArchUsesDistroIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a Unix executable")
	}

	want := platform.IsArchBased()

	withPacman := t.TempDir()
	if err := os.WriteFile(filepath.Join(withPacman, "pacman"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", withPacman)
	if got := isArch(); got != want {
		t.Errorf("isArch() = %v with pacman in PATH, want distro result %v", got, want)
	}

	t.Setenv("PATH", t.TempDir())
	if got := isArch(); got != want {
		t.Errorf("isArch() = %v without pacman in PATH, want distro result %v", got, want)
	}
}
