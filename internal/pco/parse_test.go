package pco

import (
	"errors"
	"slices"
	"testing"
)

func TestParseSongID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"31193598", "31193598"},
		{"  31193598  ", "31193598"},
		{"https://services.planningcenteronline.com/songs/31193598", "31193598"},
		{"https://services.planningcenteronline.com/songs/31193598/", "31193598"},
		{"https://services.planningcenteronline.com/songs/31193598/arrangements/36605324", "31193598"},
		{"https://services.planningcenteronline.com/songs/31193598?foo=1", "31193598"},
		{"services.planningcenteronline.com/songs/31193598", "31193598"},
	}
	for _, tc := range cases {
		got, err := ParseSongID(tc.in)
		if err != nil {
			t.Fatalf("ParseSongID(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseSongID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSongIDRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"0",
		"-3",
		"not a song",
		"https://example.com/songs/31193598",
		"https://services.planningcenteronline.com/plans/123",
	} {
		if _, err := ParseSongID(in); err == nil {
			t.Fatalf("ParseSongID(%q) succeeded", in)
		}
	}
}

func TestParseSelection(t *testing.T) {
	got, err := ParseSelection("1, 3-5, 3", 6)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 2, 3, 4}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	got, err = ParseSelection("all", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("all: %v", got)
	}

	if _, err := ParseSelection("", 3); !errors.Is(err, ErrCancel) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ParseSelection("q", 3); !errors.Is(err, ErrCancel) {
		t.Fatalf("q: %v", err)
	}
	if _, err := ParseSelection("0", 3); err == nil {
		t.Fatal("expected error for 0")
	}
	if _, err := ParseSelection("4", 3); err == nil {
		t.Fatal("expected error for past the end")
	}
	if _, err := ParseSelection("3-1", 3); err == nil {
		t.Fatal("expected error for backwards range")
	}
	if _, err := ParseSelection("1-", 3); err == nil {
		t.Fatal("expected error for open range")
	}
}
