package pco

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// YouTube reports whether link is a YouTube watch page this command can save.
func YouTube(link string) bool {
	_, ok := youtubeVideoID(link)
	return ok
}

// youtubeTarget is the canonical watch page for a YouTube attachment.
// Planning Center sets filename to "YouTube" and puts the watch page in
// remote_link or url. A stored file with a real extension is left alone.
func youtubeTarget(f File) (string, bool) {
	if storedMedia(f.Filename) {
		return "", false
	}
	for _, raw := range []string{f.Link, f.URL} {
		if id, ok := youtubeVideoID(raw); ok {
			return youtubePage(id), true
		}
	}
	return "", false
}

func storedMedia(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp3", ".m4a", ".wav", ".aac", ".flac", ".ogg", ".aiff", ".aif", ".pdf", ".jpg", ".jpeg", ".png", ".gif", ".txt":
		return true
	default:
		return false
	}
}

func youtubeVideoID(link string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	if u.Port() != "" && u.Port() != "443" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(u.EscapedPath(), "/")
	var id string
	switch host {
	case "youtu.be":
		id, _, _ = strings.Cut(path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "www.youtube-nocookie.com":
		switch {
		case path == "watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(path, "embed/"), strings.HasPrefix(path, "shorts/"), strings.HasPrefix(path, "live/"), strings.HasPrefix(path, "v/"):
			rest := path[strings.IndexByte(path, '/')+1:]
			id, _, _ = strings.Cut(rest, "/")
		default:
			return "", false
		}
	default:
		return "", false
	}
	if !youtubeID(id) {
		return "", false
	}
	return id, true
}

func youtubeID(id string) bool {
	if len(id) != 11 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func youtubePage(id string) string {
	return "https://www.youtube.com/watch?v=" + id
}

// saveYouTube writes the audio of one video as an MP3.
// ffmpeg is tried first. When it cannot fetch the video, yt-dlp is used.
// The page URL is the canonical watch URL, not the attachment string.
func (c *Client) saveYouTube(ctx context.Context, pageURL, dest string) error {
	id, ok := youtubeVideoID(pageURL)
	if !ok {
		return errors.New("refusing a URL that is not a YouTube video")
	}
	pageURL = youtubePage(id)
	outPath, err := ffmpegOutput(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return err
	}
	if err := refuseSymlink(outPath); err != nil {
		return err
	}
	ffmpegErr := c.ffmpegYouTube(ctx, pageURL, outPath)
	if ffmpegErr == nil {
		return nil
	}
	if err := c.ytDlpYouTube(ctx, pageURL, outPath); err != nil {
		return fmt.Errorf("%w; %w", ffmpegErr, err)
	}
	return nil
}

func (c *Client) ffmpegYouTube(ctx context.Context, pageURL, outPath string) error {
	tmp := outPath + ".part"
	part, err := createExclusive(tmp, 0o644)
	if err != nil {
		return err
	}
	if err := part.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	out, err := c.output(ctx, "ffmpeg", ffmpegYouTubeArgs(pageURL, tmp))
	if err != nil {
		_ = os.Remove(tmp)
		return toolError("ffmpeg", out, err)
	}
	if err := finishedMedia(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := refuseSymlink(outPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func ffmpegYouTubeArgs(pageURL, output string) []string {
	return []string{
		"-nostdin", "-y", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file,https,tls,tcp,http",
		"-i", pageURL,
		"-vn",
		"-c:a", "libmp3lame",
		"-q:a", "2",
		"-f", "mp3",
		output,
	}
}

func (c *Client) ytDlpYouTube(ctx context.Context, pageURL, outPath string) error {
	dir, err := os.MkdirTemp(filepath.Dir(outPath), ".pco-yt-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	target := filepath.Join(dir, "audio.mp3")
	out, err := c.output(ctx, "yt-dlp", ytDlpArgs(pageURL, target))
	if err != nil {
		return toolError("yt-dlp", out, err)
	}
	got, err := oneRegularFile(dir)
	if err != nil {
		return err
	}
	if err := finishedMedia(got); err != nil {
		return err
	}
	return moveFile(got, outPath)
}

func ytDlpArgs(pageURL, output string) []string {
	return []string{
		"--ignore-config",
		"--no-playlist",
		"--no-progress",
		"--no-mtime",
		"--no-write-info-json",
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "2",
		"-o", output,
		"--",
		pageURL,
	}
}

func (c *Client) output(ctx context.Context, name string, args []string) ([]byte, error) {
	if c.execCmd != nil {
		return c.execCmd(ctx, name, args)
	}
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: name is ffmpeg or yt-dlp; args are fixed plus a canonical YouTube URL
	cmd.Env = scrubSecrets(os.Environ())
	return cmd.CombinedOutput()
}

func toolError(name string, out []byte, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed", name)
	}
	msg := clip(string(out), 180)
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("%s: %s", name, msg)
}

func finishedMedia(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return errors.New("download did not produce a file")
	}
	if fi.Size() == 0 {
		return errors.New("download was empty")
	}
	if fi.Size() > maxDownload {
		return fmt.Errorf("file is larger than %d MB", maxDownload>>20)
	}
	return nil
}

func oneRegularFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var got string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", errors.New("download did not produce a file")
		}
		if got != "" {
			return "", errors.New("download produced more than one file")
		}
		got = filepath.Join(dir, e.Name())
	}
	if got == "" {
		return "", errors.New("download did not produce a file")
	}
	return got, nil
}

func moveFile(src, dest string) error {
	if err := refuseSymlink(dest); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	in, err := os.Open(src) //nolint:gosec // G304: src is the single file yt-dlp wrote in a temp directory this process created
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return saveStream(dest, in)
}
