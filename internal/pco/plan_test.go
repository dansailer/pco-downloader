package pco

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFilesForPlan(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/services/v2/service_types", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{
			"data": []any{res("1", map[string]any{"name": "Other"}), res("2", map[string]any{"name": "Sunday"})},
		})
	})
	mux.HandleFunc("/services/v2/service_types/1/plans/89693797", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		http.NotFound(w, r)
	})
	mux.HandleFunc("/services/v2/service_types/2/plans/89693797", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("89693797", map[string]any{
			"title": "Sunday Night",
			"dates": "November 29, 2026",
		})})
	})
	mux.HandleFunc("/services/v2/service_types/2/plans/89693797/items", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{
			planItem("h", "", "", ""),
			planItem("a", "10", "20", "30"),
			planItem("b", "10", "20", "30"),
			planItem("c", "10", "21", ""),
			planItem("d", "11", "", ""),
		}})
	})
	mux.HandleFunc("/services/v2/songs/10", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("10", map[string]any{"title": "You Stay The Same"})})
	})
	mux.HandleFunc("/services/v2/songs/10/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{
			att("1", "lyrics.pdf", "Lyrics", "application/pdf", 8, true),
			att("2", "chart.pdf", "Chart on song", "application/pdf", 10, true),
		}})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/20", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("20", map[string]any{"name": "Default Arrangement"})})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/20/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{
			att("2", "chart.pdf", "Chart", "application/pdf", 10, true),
			att("3", "bass.wav", "Bass", "audio/wav", 80, false),
		}})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/20/keys/30", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("30", map[string]any{"starting_key": "D"})})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/20/keys/30/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{att("4", "drums.wav", "Drums", "audio/wav", 80, false)}})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/21", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("21", map[string]any{"name": "Acoustic"})})
	})
	mux.HandleFunc("/services/v2/songs/10/arrangements/21/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{att("5", "piano.wav", "Piano", "audio/wav", 80, false)}})
	})
	mux.HandleFunc("/services/v2/songs/11", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": res("11", map[string]any{"title": "Call"})})
	})
	mux.HandleFunc("/services/v2/songs/11/attachments", func(w http.ResponseWriter, r *http.Request) {
		assertBasic(t, r)
		writeJSON(t, w, map[string]any{"data": []any{att("6", "call.pdf", "Chart", "application/pdf", 4, true)}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("app", "secret")
	c.BaseURL = srv.URL

	plan, files, err := c.FilesForPlan(context.Background(), "89693797")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Title != "Sunday Night" || plan.Dates != "November 29, 2026" || plan.ID != "89693797" {
		t.Fatalf("plan = %+v", plan)
	}
	got := make([]string, len(files))
	for i, f := range files {
		got[i] = f.ID + " " + f.SongTitle + " " + f.Arrangement + " " + f.Key
	}
	want := []string{
		"1 You Stay The Same Song ",
		"2 You Stay The Same Default Arrangement ",
		"3 You Stay The Same Default Arrangement ",
		"4 You Stay The Same Default Arrangement D",
		"5 You Stay The Same Acoustic ",
		"6 Call Song ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s", strings.Join(got, "\n"))
	}
	if files[2].Access() != "stream-only" || files[1].Access() != "download" {
		t.Fatalf("access = %s %s", files[1].Access(), files[2].Access())
	}
}

func TestFilesForPlanMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/services/v2/service_types", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"data": []any{res("1", nil), res("2", nil)}})
	})
	mux.HandleFunc("/services/v2/service_types/1/plans/5", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/services/v2/service_types/2/plans/5", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("app", "secret")
	c.BaseURL = srv.URL
	_, _, err := c.FilesForPlan(context.Background(), "5")
	if err == nil || !strings.Contains(err.Error(), "not in a service type") {
		t.Fatal(err)
	}
}

func TestFilesForPlanStopsOnAuthError(t *testing.T) {
	var sawSecond bool
	mux := http.NewServeMux()
	mux.HandleFunc("/services/v2/service_types", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"data": []any{res("1", nil), res("2", nil)}})
	})
	mux.HandleFunc("/services/v2/service_types/1/plans/5", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"detail":"unauthorized"}]}`))
	})
	mux.HandleFunc("/services/v2/service_types/2/plans/5", func(w http.ResponseWriter, r *http.Request) {
		sawSecond = true
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("app", "secret")
	c.BaseURL = srv.URL
	_, _, err := c.FilesForPlan(context.Background(), "5")
	if err == nil {
		t.Fatal("expected auth error")
	}
	if sawSecond {
		t.Fatal("kept searching after an authorization error")
	}
}

func planItem(id, songID, arrID, keyID string) map[string]any {
	rel := map[string]any{
		"song":        relData(songID, "Song"),
		"arrangement": relData(arrID, "Arrangement"),
		"key":         relData(keyID, "Key"),
	}
	return map[string]any{"type": "Item", "id": id, "attributes": map[string]any{}, "relationships": rel}
}

func relData(id, kind string) map[string]any {
	if id == "" {
		return map[string]any{"data": nil}
	}
	return map[string]any{"data": map[string]any{"type": kind, "id": id}}
}
