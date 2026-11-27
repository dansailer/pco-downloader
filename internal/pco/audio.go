package pco

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

type mediaKind int

const (
	mediaUnknown mediaKind = iota
	mediaMP3
	mediaOther
	mediaHLS
	mediaNotAudio
)

// saveAudio writes the playback URL to dest as an MP3.
// An MP3 stream is copied. Anything else audio-like is converted with ffmpeg.
func (c *Client) saveAudio(ctx context.Context, fileURL, dest string) error {
	resp, err := c.getFile(ctx, fileURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("playback HTTP %d", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	head, err := br.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return err
	}
	final := fileURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	switch classify(resp.Header.Get("Content-Type"), final, head) {
	case mediaNotAudio:
		return fmt.Errorf("playback URL did not return audio (%s)", resp.Header.Get("Content-Type"))
	case mediaMP3:
		return saveStream(dest, br)
	case mediaHLS:
		return c.saveHLS(ctx, final, dest)
	default:
		tmp, err := spool(br)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmp) }()
		return c.toMP3(ctx, tmp, dest)
	}
}

func (c *Client) getFile(ctx context.Context, raw string) (*http.Response, error) {
	if err := c.validateFetchURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	// Signed file hosts must not see the API token. A stream that is still on
	// the API host needs it.
	if apiAuthedFile(c.base(), raw) {
		req.SetBasicAuth(c.AppID, c.Secret)
	}
	return c.httpClient().Do(req)
}

func apiAuthedFile(base, raw string) bool {
	b, err1 := url.Parse(base)
	u, err2 := url.Parse(raw)
	if err1 != nil || err2 != nil || b.User != nil || u.User != nil {
		return false
	}
	if !sameEndpoint(b, u) {
		return false
	}
	if u.Scheme != "https" && !isLocalName(u.Hostname()) {
		return false
	}
	cleaned := path.Clean(u.Path)
	return cleaned == "/services" || strings.HasPrefix(cleaned, "/services/")
}

func (c *Client) saveRaw(ctx context.Context, fileURL, dest string) error {
	resp, err := c.getFile(ctx, fileURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	final := fileURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	if id, ok := youtubeVideoID(final); ok {
		_ = resp.Body.Close()
		return c.saveYouTube(ctx, youtubePage(id), dest)
	}
	return saveStream(dest, resp.Body)
}

func classify(contentType, rawURL string, head []byte) mediaKind {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	path := ""
	if u, err := url.Parse(rawURL); err == nil {
		path = strings.ToLower(u.Path)
	}
	trimmed := bytes.TrimSpace(head)
	if bytes.HasPrefix(trimmed, []byte("<")) || bytes.HasPrefix(trimmed, []byte("{")) {
		return mediaNotAudio
	}
	if ct == "application/vnd.apple.mpegurl" || ct == "application/x-mpegurl" || ct == "audio/mpegurl" ||
		strings.HasSuffix(path, ".m3u8") || bytes.HasPrefix(trimmed, []byte("#EXTM3U")) {
		return mediaHLS
	}
	if ct == "audio/mpeg" || ct == "audio/mp3" || isMP3(head) {
		return mediaMP3
	}
	if strings.HasPrefix(ct, "audio/") || ct == "video/mp4" || ct == "application/mp4" || isContainer(head) {
		return mediaOther
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mediaMP3
	case ".m4a", ".aac", ".wav", ".flac", ".ogg", ".oga", ".mp4", ".aif", ".aiff":
		return mediaOther
	default:
		return mediaUnknown
	}
}

func isMP3(b []byte) bool {
	if bytes.HasPrefix(b, []byte("ID3")) {
		return true
	}
	return len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0
}

func isContainer(b []byte) bool {
	if bytes.HasPrefix(b, []byte("RIFF")) || bytes.HasPrefix(b, []byte("OggS")) || bytes.HasPrefix(b, []byte("fLaC")) {
		return true
	}
	return len(b) >= 8 && bytes.Equal(b[4:8], []byte("ftyp"))
}

func spool(r io.Reader) (string, error) {
	f, err := os.CreateTemp("", "pco-dl-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	copyErr := copyLimited(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(name)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(name)
		return "", closeErr
	}
	return name, nil
}

// saveHLS downloads every segment with the guarded HTTP client, then converts
// the local bytes. ffmpeg never opens a network URL.
func (c *Client) saveHLS(ctx context.Context, playlistURL, dest string) error {
	body, err := c.readLimited(ctx, playlistURL, 2<<20)
	if err != nil {
		return err
	}
	text := string(body)
	mediaURL := playlistURL
	if variant, ok := pickVariant(playlistURL, text); ok {
		if err := c.validateFetchURL(variant); err != nil {
			return err
		}
		body, err = c.readLimited(ctx, variant, 2<<20)
		if err != nil {
			return err
		}
		mediaURL = variant
		text = string(body)
	}
	segs, err := segmentURLs(mediaURL, text)
	if err != nil {
		return err
	}
	for _, seg := range segs {
		if err := c.validateFetchURL(seg); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp("", "pco-dl-hls-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	var total int64
	for _, seg := range segs {
		n, err := c.appendURL(ctx, seg, tmp, maxDownload-total)
		total += n
		if err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return c.toMP3(ctx, name, dest)
}

func (c *Client) readLimited(ctx context.Context, raw string, limit int64) ([]byte, error) {
	resp, err := c.getFile(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("playback HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("playlist is too large")
	}
	return body, nil
}

func (c *Client) appendURL(ctx context.Context, raw string, f *os.File, budget int64) (int64, error) {
	if budget <= 0 {
		return 0, fmt.Errorf("file is larger than %d MB", maxDownload>>20)
	}
	resp, err := c.getFile(ctx, raw)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("playback HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, budget+1))
	if n > budget {
		return n, fmt.Errorf("file is larger than %d MB", maxDownload>>20)
	}
	return n, err
}

func pickVariant(base, text string) (string, bool) {
	if !strings.Contains(text, "#EXT-X-STREAM-INF") {
		return "", false
	}
	var best string
	var bestBW int
	var pending bool
	var pendingBW int
	found := false
	for _, line := range splitLines(text) {
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			pending = true
			pendingBW = bandwidth(line)
			continue
		}
		if pending && line != "" && !strings.HasPrefix(line, "#") {
			if !found || pendingBW >= bestBW {
				best = line
				bestBW = pendingBW
				found = true
			}
			pending = false
		}
	}
	if !found {
		return "", false
	}
	return resolveRef(base, best), true
}

func bandwidth(line string) int {
	const key = "BANDWIDTH="
	i := strings.Index(line, key)
	if i < 0 {
		return 0
	}
	rest := line[i+len(key):]
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func segmentURLs(base, text string) ([]string, error) {
	var out []string
	for _, line := range splitLines(text) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, resolveRef(base, line))
	}
	if len(out) == 0 {
		return nil, errors.New("playlist has no segments")
	}
	if len(out) > 2000 {
		return nil, errors.New("playlist has too many segments")
	}
	return out, nil
}

func splitLines(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = strings.TrimSpace(strings.TrimRight(line, "\r"))
	}
	return out
}

func resolveRef(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func (c *Client) toMP3(ctx context.Context, src, dest string) error {
	if err := localMediaPath(src); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	if c.transcode != nil {
		return c.transcode(ctx, src, dest)
	}
	return ffmpegToMP3(ctx, src, dest)
}

func localMediaPath(src string) error {
	if strings.Contains(src, "://") || strings.ContainsAny(src, "\r\n") {
		return errors.New("refusing to transcode a remote path")
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	tmp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(tmp, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("refusing to transcode a file outside the temp directory")
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return errors.New("refusing to transcode a non-regular file")
	}
	return nil
}

func ffmpegToMP3(ctx context.Context, src, dest string) error {
	if err := localMediaPath(src); err != nil {
		return err
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
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
	tmp := outPath + ".part"
	part, err := createExclusive(tmp, 0o644)
	if err != nil {
		return err
	}
	if err := part.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// -f mp3 is required. The temp name ends in .part, so ffmpeg cannot infer MP3 from it.
	cmd := exec.CommandContext(ctx, "ffmpeg", ffmpegArgs(abs, tmp)...) //nolint:gosec // G204: flags are fixed; the path passed localMediaPath
	cmd.Env = scrubSecrets(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("ffmpeg is required to save this stream as mp3")
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", msg)
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

func ffmpegArgs(input, output string) []string {
	return []string{
		"-nostdin", "-y", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file,pipe,crypto",
		"-i", "file:" + filepath.ToSlash(input),
		"-vn",
		"-c:a", "libmp3lame",
		"-q:a", "2",
		"-f", "mp3",
		output,
	}
}

// ffmpegOutput is an absolute path. A relative name that starts with "-"
// would otherwise be another ffmpeg option.
func ffmpegOutput(dest string) (string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(abs) || strings.HasPrefix(abs, "-") {
		return "", errors.New("refusing an output path ffmpeg could read as an option")
	}
	return abs, nil
}

func scrubSecrets(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		switch strings.ToLower(key) {
		case "pco_app_id", "pco_secret", "http_proxy", "https_proxy", "all_proxy":
			continue
		}
		out = append(out, e)
	}
	return out
}
