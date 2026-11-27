package pco

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesForSongAndDownload(t *testing.T) {
	var fileAuth string
	var opened []string
	mux := http.NewServeMux()

	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		fileAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("bytes-" + strings.TrimPrefix(r.URL.Path, "/file/")))
	})

	mux.HandleFunc("/services/v2/songs/31193598/arrangements/36605324/keys/9/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		if r.URL.Query().Get("offset") == "1" {
			writeJSON(t, w, map[string]any{
				"data": []any{
					att("3", "Guide.mp3", "Guide vocal", "audio/mpeg", 20, false),
					attLink("4", "Spotify", "https://open.spotify.com/track/abc"),
				},
			})
			return
		}
		writeJSON(t, w, map[string]any{
			"data": []any{att("2", "Practice.mp3", "Practice", "audio/mpeg", 40, true)},
			"links": map[string]string{
				"next": "http://" + r.Host + r.URL.Path + "?offset=1&per_page=100",
			},
		})
	})

	mux.HandleFunc("/services/v2/songs/31193598/arrangements/36605324/keys", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": []any{res("9", map[string]any{"name": "Default", "starting_key": "G"})},
		})
	})

	mux.HandleFunc("/services/v2/songs/31193598/arrangements/36605324/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": []any{att("1", "Chart.pdf", "Chord chart", "application/pdf", 80, false)},
		})
	})

	mux.HandleFunc("/services/v2/songs/31193598/arrangements", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": []any{res("36605324", map[string]any{"name": "Default"})},
		})
	})

	mux.HandleFunc("/services/v2/songs/31193598/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": []any{att("1", "Chart.pdf", "Chord chart", "application/pdf", 80, false)},
		})
	})

	mux.HandleFunc("/services/v2/songs/31193598", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": res("31193598", map[string]any{"title": "Great Are You Lord", "author": "Sons"}),
		})
	})

	mux.HandleFunc("/services/v2/attachments/", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/open") {
			t.Errorf("open request = %s %s", r.Method, r.URL.Path)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/services/v2/attachments/"), "/open")
		opened = append(opened, id)
		writeJSON(t, w, map[string]any{
			"data": res("act", map[string]any{"attachment_url": "http://" + r.Host + "/file/" + id}),
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("app", "secret")
	c.BaseURL = srv.URL

	song, files, err := c.FilesForSong(context.Background(), "31193598")
	if err != nil {
		t.Fatal(err)
	}
	if song.Title != "Great Are You Lord" || song.ID != "31193598" {
		t.Fatalf("song = %+v", song)
	}
	if len(files) != 4 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].ID != "1" || files[0].Access() != "download" || files[0].Arrangement != "Default" {
		t.Fatalf("chart = %+v", files[0])
	}
	if files[1].ID != "2" || files[1].Key != "G" || files[1].Access() != "download" {
		t.Fatalf("practice = %+v", files[1])
	}
	if files[2].Access() != "stream-only" || files[2].Key != "G" {
		t.Fatalf("guide = %+v", files[2])
	}
	if files[3].Access() != "link" || files[3].Link != "https://open.spotify.com/track/abc" {
		t.Fatalf("link = %+v", files[3])
	}
	// The chart is also on the song. The arrangement copy wins, so it is not listed twice.
	for _, f := range files {
		if f.Arrangement == "Song" {
			t.Fatalf("duplicate song attachment listed: %+v", f)
		}
	}

	dir := t.TempDir()
	dest, err := DestPath(dir, song.Title, files[2])
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Download(context.Background(), files[2], dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bytes-3" {
		t.Fatalf("body = %q", got)
	}
	if fileAuth != "" {
		t.Fatalf("file request sent Authorization %q", fileAuth)
	}
	if strings.Join(opened, ",") != "3" {
		t.Fatalf("opened %v", opened)
	}
	if err := c.Download(context.Background(), files[3], dest); err == nil {
		t.Fatal("link download succeeded")
	}
}

func TestListRejectsForeignNext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/services/v2/songs/5":
			writeJSON(t, w, map[string]any{"data": res("5", map[string]any{"title": "T"})})
		case strings.HasSuffix(r.URL.Path, "/arrangements"):
			writeJSON(t, w, map[string]any{
				"data":  []any{},
				"links": map[string]string{"next": "https://evil.example/steal"},
			})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New("app", "secret")
	c.BaseURL = srv.URL
	_, _, err := c.FilesForSong(context.Background(), "5")
	if err == nil || !strings.Contains(err.Error(), "evil.example") {
		t.Fatalf("err = %v", err)
	}
}

func TestDestPathStaysInsideRoot(t *testing.T) {
	dir := t.TempDir()
	f := File{Arrangement: "../etc", Key: "..", Filename: "../../x.mp3"}
	got, err := DestPath(dir, "../secret", f)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, got)
	if err != nil {
		t.Fatal(err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("path escaped: %s", got)
	}
	if !strings.HasSuffix(got, "x.mp3") {
		t.Fatalf("path = %s", got)
	}
}

func assertBasic(t *testing.T, r *http.Request) {
	t.Helper()
	user, pass, ok := r.BasicAuth()
	if !ok || user != "app" || pass != "secret" {
		t.Errorf("basic auth = %q %q ok=%v", user, pass, ok)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func res(id string, attrs map[string]any) map[string]any {
	return map[string]any{"type": "X", "id": id, "attributes": attrs}
}

func att(id, filename, display, contentType string, size int64, allow bool) map[string]any {
	return res(id, map[string]any{
		"filename":           filename,
		"display_name":       display,
		"content_type":       contentType,
		"file_size":          size,
		"allow_mp3_download": allow,
		"downloadable":       true,
	})
}

func attLink(id, display, link string) map[string]any {
	return res(id, map[string]any{
		"display_name":       display,
		"remote_link":        link,
		"allow_mp3_download": false,
		"downloadable":       false,
	})
}
