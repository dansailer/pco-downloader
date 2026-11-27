package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCredFile(t *testing.T) {
	id, secret, err := parseCredFile("# comment\n\nPCO_APP_ID=app\nPCO_SECRET=\"sek ret\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if id != "app" || secret != "sek ret" {
		t.Fatalf("id=%q secret=%q", id, secret)
	}
	if _, _, err := parseCredFile("NOPE=1\n"); err == nil {
		t.Fatal("expected unknown key error")
	}
	bare := "supersecret-without-equals"
	_, _, err = parseCredFile(bare + "\n")
	if err == nil || strings.Contains(err.Error(), bare) {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveCredsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pco-downloader", "credentials")
	if err := saveCreds(path, "app", "sek"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", fi.Mode().Perm())
	}
	di, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", di.Mode().Perm())
	}
	id, secret, ok, err := readCredFile(path)
	if err != nil || !ok || id != "app" || secret != "sek" {
		t.Fatalf("read id=%q secret=%q ok=%v err=%v", id, secret, ok, err)
	}

	loose := filepath.Join(dir, "pco-downloader", "loose")
	if err := os.WriteFile(loose, []byte("PCO_APP_ID=app\nPCO_SECRET=sek\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readCredFile(loose); err != nil {
		t.Fatal(err)
	}
	fi, err = os.Lstat(loose)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("loose mode = %o", fi.Mode().Perm())
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := saveCreds(link, "app", "other"); err == nil {
		t.Fatal("save followed a symlink")
	}
	if _, _, _, err := readCredFile(link); err == nil {
		t.Fatal("read followed a symlink")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "PCO_APP_ID=app\nPCO_SECRET=sek\n" {
		t.Fatalf("credentials changed through the symlink: %q", got)
	}
}
