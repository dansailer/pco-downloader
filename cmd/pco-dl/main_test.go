package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadsDir(t *testing.T) {
	home := t.TempDir()
	if _, err := downloadsDir(home); err == nil {
		t.Fatal("missing Downloads was accepted")
	}

	dir := filepath.Join(home, "Downloads")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := downloadsDir(home)
	if err != nil || got != dir {
		t.Fatalf("got %q err %v", got, err)
	}

	real := filepath.Join(home, "real-downloads")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, dir); err != nil {
		t.Fatal(err)
	}
	got, err = downloadsDir(home)
	if err != nil || got != dir {
		t.Fatalf("inside symlink: got %q err %v", got, err)
	}

	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadsDir(home); err == nil {
		t.Fatal("symlink outside home was accepted")
	}
}
