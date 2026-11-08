package announce

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchParsesAllAndSplits(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"announcements":[
			{"id":"a1","title":"维护","level":"warn","start":"2026-01-01"},
			{"id":"a2","title":"过期","level":"info","start":"2026-01-01","end":"2026-01-31"},
			{"id":"a3","title":"未来","level":"info","start":"2099-01-01"},
			{"id":"","title":"无id"},
			{"id":"a4","title":"紧急","level":"critical"}
		]}`))
	}))
	defer up.Close()
	items, err := Fetch(context.Background(), up.URL, up.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("fetch = %d items, want 4 valid", len(items))
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	active := Active(items, now)
	if len(active) != 2 || active[0].ID != "a4" || active[1].ID != "a1" {
		t.Fatalf("active = %+v", active)
	}
	ended := EndedItems(items, now)
	if len(ended) != 1 || ended[0].ID != "a2" {
		t.Fatalf("ended = %+v", ended)
	}
}

func TestFetch404IsEmpty(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer up.Close()
	items, err := Fetch(context.Background(), up.URL, up.Client())
	if err != nil {
		t.Fatalf("404 must not be an error, got %v", err)
	}
	if items != nil {
		t.Fatalf("items = %v, want nil", items)
	}
}

func TestFetchBadJSON(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer up.Close()
	if _, err := Fetch(context.Background(), up.URL, up.Client()); err == nil {
		t.Fatal("want error for non-JSON body")
	}
}

func TestFetchGitHubContentsEnvelope(t *testing.T) {
	inner := `{"announcements":[{"id":"c1","title":"来自 Contents API","level":"warn"}]}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte(inner)) + `","encoding":"base64"}`))
	}))
	defer up.Close()
	items, err := Fetch(context.Background(), up.URL, up.Client())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "c1" || items[0].Level != LevelWarn {
		t.Fatalf("items = %+v", items)
	}
}

func TestParseDefaultsLevel(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	items, err := Parse([]byte(`{"announcements":[{"id":"x","title":"t","level":"weird"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	out := Active(items, now)
	if len(out) != 1 || out[0].Level != LevelInfo {
		t.Fatalf("out = %+v", out)
	}
}
