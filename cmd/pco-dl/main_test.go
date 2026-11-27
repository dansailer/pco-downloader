package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansailer/pco-downloader/internal/pco"
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

func TestPrintFilesPlanColumn(t *testing.T) {
	var buf strings.Builder
	files := []pco.File{{
		SongTitle:   "You Stay The Same",
		Arrangement: "Default Arrangement",
		DisplayName: "Chart",
		Filename:    "chart.pdf",
		Bytes:       10,
	}}
	if err := printFiles(&buf, "Sunday Night", "89693797", "November 29, 2026", files, true); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"Sunday Night (89693797)",
		"November 29, 2026",
		"Song",
		"You Stay The Same",
		"download",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPrintFilesSongOmitsSongColumn(t *testing.T) {
	var buf strings.Builder
	files := []pco.File{{Arrangement: "Default", DisplayName: "Chart", Filename: "chart.pdf", Bytes: 8}}
	if err := printFiles(&buf, "Call", "11", "Author", files, false); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	var header string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "#") {
			header = line
			break
		}
	}
	if strings.Contains(header, "Song") {
		t.Fatalf("song listing gained a Song column:\n%s", got)
	}
	if !strings.Contains(got, "Call (11)") || !strings.Contains(got, "Author") {
		t.Fatalf("song heading missing:\n%s", got)
	}
}

func TestSongNameUsesFileTitle(t *testing.T) {
	if got := songName(pco.File{SongTitle: "Call"}, "Sunday Night"); got != "Call" {
		t.Fatalf("got %q", got)
	}
	if got := songName(pco.File{}, "Sunday Night"); got != "Sunday Night" {
		t.Fatalf("fallback got %q", got)
	}
}
