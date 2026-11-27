package pco

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DestPath is a single file in root named "<song>_<filename>".
// The result stays inside root and is one path segment.
func DestPath(root, songTitle string, f File) (string, error) {
	name := limitFileName(sanitizeSegment(songTitle)+"_"+sanitizeFile(outputName(f)), 180)
	root = filepath.Clean(root)
	full := filepath.Join(root, name)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.Contains(rel, string(os.PathSeparator)) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("refusing to write outside %s", root)
	}
	return full, nil
}

// UniquePath returns path, or path with a numeric suffix when that name is taken.
// Symlinks count as taken so a later write cannot follow one.
func UniquePath(path string) (string, error) {
	taken, err := nameTaken(path)
	if err != nil {
		return "", err
	}
	if !taken {
		return path, nil
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for n := 2; n < 1000; n++ {
		candidate := fmt.Sprintf("%s-%d%s", base, n, ext)
		taken, err := nameTaken(candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("too many existing files for %s", filepath.Base(path))
}

func nameTaken(path string) (bool, error) {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func limitFileName(name string, max int) string {
	if len(name) <= max {
		return name
	}
	ext := filepath.Ext(name)
	if len(ext) > 16 || len(ext) >= max {
		ext = ""
	}
	cut := name[:max-len(ext)]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	cut = strings.TrimRight(cut, " .")
	if cut == "" {
		cut = "file"
	}
	return cut + ext
}

func outputName(f File) string {
	name := f.Filename
	if name == "" {
		name = f.DisplayName
	}
	if name == "" {
		name = "attachment-" + f.ID
	}
	if !isAudio(f.ContentType, name) {
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		base = "audio"
	}
	return base + ".mp3"
}

func sanitizeSegment(s string) string {
	s = strings.TrimSpace(s)
	out := sanitize(s)
	out = strings.Trim(out, " .")
	if out == "" || out == "." || out == ".." {
		return "untitled"
	}
	return out
}

func sanitizeFile(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	ext := filepath.Ext(s)
	base := strings.TrimSuffix(s, ext)
	base = strings.Trim(sanitize(base), " .")
	ext = sanitize(ext)
	if base == "" || base == "." || base == ".." {
		base = "file"
	}
	return base + ext
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			b.WriteByte('-')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
