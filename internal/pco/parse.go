package pco

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ErrCancel means the user left the selection prompt without choosing files.
var ErrCancel = errors.New("cancelled")

// ParseSongID accepts a Services song id or a song page URL.
// An arrangement URL is accepted and the song id is taken from it.
func ParseSongID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", errors.New("song id or URL is required")
	}
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		if id <= 0 {
			return "", fmt.Errorf("song id %q is not valid", s)
		}
		return s, nil
	}

	raw := s
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("song id or URL is not valid: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host != "services.planningcenteronline.com" && !strings.HasSuffix(host, ".planningcenteronline.com") {
		return "", fmt.Errorf("URL host %q is not a Planning Center Services song page", u.Hostname())
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "songs" || !isPositiveID(parts[1]) {
		return "", fmt.Errorf("URL %q has no /songs/{id} path", s)
	}
	return parts[1], nil
}

func isPositiveID(s string) bool {
	id, err := strconv.ParseInt(s, 10, 64)
	return err == nil && id > 0
}

// ParseSelection reads a prompt answer such as "1,3-5" or "all".
// Returned indexes are zero-based, unique, and ordered.
func ParseSelection(input string, n int) ([]int, error) {
	s := strings.TrimSpace(input)
	if s == "" || strings.EqualFold(s, "q") || strings.EqualFold(s, "quit") {
		return nil, ErrCancel
	}
	if n <= 0 {
		return nil, errors.New("there are no files to select")
	}
	if strings.EqualFold(s, "all") {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}

	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("selection %q has an empty entry", input)
		}
		lo, hi, err := parseRange(part, n)
		if err != nil {
			return nil, err
		}
		for i := lo; i <= hi; i++ {
			if !seen[i] {
				seen[i] = true
				out = append(out, i)
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrCancel
	}
	return out, nil
}

func parseRange(part string, n int) (int, int, error) {
	left, right, ok := strings.Cut(part, "-")
	if !ok {
		i, err := parseIndex(part, n)
		return i, i, err
	}
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return 0, 0, fmt.Errorf("selection %q is not a range", part)
	}
	lo, err := parseIndex(left, n)
	if err != nil {
		return 0, 0, err
	}
	hi, err := parseIndex(right, n)
	if err != nil {
		return 0, 0, err
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("selection %q runs backwards", part)
	}
	return lo, hi, nil
}

func parseIndex(s string, n int) (int, error) {
	s = strings.TrimSpace(s)
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("selection %q is not a file number", s)
	}
	if v > n {
		return 0, fmt.Errorf("selection %d is past the last file (%d)", v, n)
	}
	return v - 1, nil
}
