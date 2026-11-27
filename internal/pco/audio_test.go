package pco

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	if got := classify("audio/mpeg", "https://cdn.example/a", []byte("not really")); got != mediaMP3 {
		t.Fatalf("mpeg content type: %v", got)
	}
	if got := classify("application/octet-stream", "https://cdn.example/a.bin", []byte("ID3rest")); got != mediaMP3 {
		t.Fatalf("id3: %v", got)
	}
	if got := classify("application/vnd.apple.mpegurl", "https://cdn.example/a", []byte("#EXTM3U\n")); got != mediaHLS {
		t.Fatalf("hls: %v", got)
	}
	if got := classify("audio/mp4", "https://cdn.example/a.m4a", []byte("xxxxftyp")); got != mediaOther {
		t.Fatalf("m4a: %v", got)
	}
	if got := classify("text/html", "https://cdn.example/a", []byte("<!doctype html>")); got != mediaNotAudio {
		t.Fatalf("html: %v", got)
	}
}

func TestDestPathAudioIsMP3(t *testing.T) {
	dir := t.TempDir()
	f := File{Arrangement: "Default", Key: "G", Filename: "Guide.m4a", ContentType: "audio/mp4"}
	got, err := DestPath(dir, "Song", f)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "Song_Guide.mp3") {
		t.Fatalf("path = %s", got)
	}
	f.Filename = "05_Bass rehearsal.mp3"
	f.ContentType = "audio/mpeg"
	got, err = DestPath(dir, "Here I Am", f)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "Here I Am_05_Bass rehearsal.mp3") {
		t.Fatalf("path = %s", got)
	}
}

func TestDownloadConvertsStreamToMP3(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/services/v2/attachments/8/open", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": res("act", map[string]any{"attachment_url": "http://" + r.Host + "/live.m3u8"}),
		})
	})
	mux.HandleFunc("/live.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("playlist request sent Authorization")
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:1,\nseg.ts\n"))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("segment request sent Authorization")
		}
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("segment-bytes"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var src string
	c := New("app", "secret")
	c.BaseURL = srv.URL
	c.transcode = func(ctx context.Context, in, dest string) error {
		src = in
		body, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "segment-bytes") {
			return fmt.Errorf("media file = %q", body)
		}
		return os.WriteFile(dest, []byte("ID3converted"), 0o644)
	}
	dest := filepath.Join(t.TempDir(), "Guide.mp3")
	f := File{ID: "8", Filename: "Guide.mp3", ContentType: "audio/mpeg", AllowMP3Download: false}
	if err := c.Download(context.Background(), f, dest); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src, "://") || src == "" {
		t.Fatalf("transcode src = %s", src)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ID3converted" {
		t.Fatalf("body = %q", got)
	}
}

func TestDownloadFallsBackToAttachmentURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/services/v2/attachments/9/open", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"status":"403","detail":"stream only"}]}`))
	})
	mux.HandleFunc("/file/9", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("file request sent Authorization")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3fallback"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("app", "secret")
	c.BaseURL = srv.URL
	dest := filepath.Join(t.TempDir(), "Guide.mp3")
	f := File{
		ID:          "9",
		Filename:    "Guide.mp3",
		ContentType: "audio/mpeg",
		URL:         srv.URL + "/file/9",
	}
	if err := c.Download(context.Background(), f, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ID3fallback" {
		t.Fatalf("body = %q", got)
	}
}

func TestScrubSecrets(t *testing.T) {
	got := scrubSecrets([]string{"PCO_APP_ID=a", "PATH=/bin", "pco_secret=b", "HOME=/x", "HTTPS_PROXY=http://127.0.0.1:9"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/x" {
		t.Fatalf("env = %#v", got)
	}
}

func TestAPIAuthedFile(t *testing.T) {
	base := "https://api.planningcenteronline.com"
	allowed := []string{
		base + "/services/v2/attachments/1",
		base + "/services/../services/v2/x",
	}
	for _, raw := range allowed {
		if !apiAuthedFile(base, raw) {
			t.Errorf("expected auth for %s", raw)
		}
	}
	blocked := []string{
		"http://api.planningcenteronline.com/services/v2/attachments/1",
		base + ":444/services/v2/attachments/1",
		base + "/services/../account",
		"https://cdn.example/services/v2/attachments/1",
		"https://user:pass@api.planningcenteronline.com/services/v2/x",
	}
	for _, raw := range blocked {
		if apiAuthedFile(base, raw) {
			t.Errorf("sent auth to %s", raw)
		}
	}
	local := "http://127.0.0.1:9"
	if !apiAuthedFile(local, local+"/services/v2/x") {
		t.Fatal("local API file was not authenticated")
	}
}

func TestFFmpegOutputIsAbsolute(t *testing.T) {
	got, err := ffmpegOutput("-hide_banner.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || strings.HasPrefix(got, "-") {
		t.Fatalf("output = %s", got)
	}
}
