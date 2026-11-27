package pco

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYouTubeVideoID(t *testing.T) {
	ok := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=PL",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
	}
	for _, in := range ok {
		id, got := youtubeVideoID(in)
		if !got || id != "dQw4w9WgXcQ" {
			t.Fatalf("%s -> %q %v", in, id, got)
		}
	}
	bad := []string{
		"",
		"https://open.spotify.com/track/abc",
		"https://evil.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com.evil.test/watch?v=dQw4w9WgXcQ",
		"http://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://user:pass@www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/playlist?list=PL",
		"https://youtu.be/",
		"file:///etc/passwd",
	}
	for _, in := range bad {
		if YouTube(in) {
			t.Fatalf("accepted %s", in)
		}
	}
}

func TestYouTubeArgsStayCanonical(t *testing.T) {
	page := youtubePage("dQw4w9WgXcQ")
	ff := ffmpegYouTubeArgs(page, "/tmp/out.mp3.part")
	if !containsArg(ff, page) || containsArg(ff, "list=PL") {
		t.Fatalf("ffmpeg args = %q", ff)
	}
	if ff[len(ff)-3] != "-f" || ff[len(ff)-2] != "mp3" || ff[len(ff)-1] != "/tmp/out.mp3.part" {
		t.Fatalf("ffmpeg tail = %q", ff[len(ff)-3:])
	}
	yd := ytDlpArgs(page, "/tmp/audio.mp3")
	if yd[0] != "--ignore-config" || yd[len(yd)-2] != "--" || yd[len(yd)-1] != page {
		t.Fatalf("yt-dlp args = %q", yd)
	}
	transcode := ffmpegArgs("/tmp/in.wav", "/tmp/out.mp3.part")
	joined := strings.Join(transcode, " ")
	if !strings.Contains(joined, "file,pipe,crypto") || strings.Contains(joined, "https") {
		t.Fatalf("transcode args gained a network protocol: %q", transcode)
	}
}

func TestDestPathYouTubeIsMP3(t *testing.T) {
	dir := t.TempDir()
	f := File{Filename: "YouTube", DisplayName: "YouTube", Link: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	got, err := DestPath(dir, "I Thank God", f)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "I Thank God_YouTube.mp3") {
		t.Fatalf("path = %s", got)
	}
	if f.Access() != "youtube" {
		t.Fatalf("access = %s", f.Access())
	}
}

func TestDownloadYouTubeFilenameDoesNotSaveThePage(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "I Thank God_YouTube.mp3")
	c := New("app", "secret")
	c.execCmd = func(_ context.Context, name string, args []string) ([]byte, error) {
		if name != "ffmpeg" {
			t.Fatalf("tool %s", name)
		}
		if err := os.WriteFile(args[len(args)-1], []byte("audio"), 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}
	f := File{
		Filename:    "YouTube",
		DisplayName: "YouTube",
		Link:        "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		URL:         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		ContentType: "text/html",
	}
	if err := c.Download(context.Background(), f, dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || string(body) != "audio" {
		t.Fatalf("body %q err %v", body, err)
	}
}

func TestDownloadYouTubeUsesFFmpegThenYtDlp(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "song.mp3")
	var calls []string
	c := New("app", "secret")
	c.execCmd = func(_ context.Context, name string, args []string) ([]byte, error) {
		calls = append(calls, name)
		if name == "ffmpeg" {
			if !containsArg(args, "https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
				t.Fatalf("ffmpeg url = %q", args)
			}
			return []byte("unable to open"), errors.New("exit status 1")
		}
		for i, a := range args {
			if a == "-o" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte("mp3-bytes"), 0o644); err != nil {
					return nil, err
				}
				return nil, nil
			}
		}
		return nil, errors.New("missing -o")
	}
	f := File{DisplayName: "Video", Link: "https://youtu.be/dQw4w9WgXcQ?si=track"}
	if err := c.Download(context.Background(), f, dest); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "ffmpeg" || calls[1] != "yt-dlp" {
		t.Fatalf("calls = %v", calls)
	}
	body, err := os.ReadFile(dest)
	if err != nil || string(body) != "mp3-bytes" {
		t.Fatalf("body %q err %v", body, err)
	}
}

func TestDownloadYouTubeStopsWhenFFmpegWritesAudio(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "song.mp3")
	c := New("app", "secret")
	c.execCmd = func(_ context.Context, name string, args []string) ([]byte, error) {
		if name != "ffmpeg" {
			t.Fatalf("second tool %s", name)
		}
		if err := os.WriteFile(args[len(args)-1], []byte("ffmpeg-mp3"), 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}
	f := File{Link: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
	if err := c.Download(context.Background(), f, dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || string(body) != "ffmpeg-mp3" {
		t.Fatalf("body %q err %v", body, err)
	}
}

func TestDownloadSpotifyStaysALink(t *testing.T) {
	c := New("app", "secret")
	c.execCmd = func(context.Context, string, []string) ([]byte, error) {
		t.Fatal("spotify was downloaded")
		return nil, nil
	}
	f := File{DisplayName: "Spotify", Link: "https://open.spotify.com/track/abc"}
	err := c.Download(context.Background(), f, filepath.Join(t.TempDir(), "x.mp3"))
	if err == nil {
		t.Fatal("expected link error")
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.Contains(a, want) {
			return true
		}
	}
	return false
}
