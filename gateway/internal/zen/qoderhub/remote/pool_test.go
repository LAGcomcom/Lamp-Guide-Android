package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func poolCredential(id string) Credential {
	return Credential{UserID: id, MachineID: "machine-" + id, CosyKey: "key-" + id, EncryptUserInfo: "info-" + id, AccessToken: "token-" + id}
}
func poolFrame(text string) string {
	inner, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
	outer, _ := json.Marshal(map[string]any{"body": string(inner)})
	return "data: " + string(outer) + "\n\n"
}

func TestPoolQuotaSwitchStickyAndModelCooldown(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Cosy-User")
		model := r.Header.Get("X-Model-Key")
		mu.Lock()
		calls = append(calls, id+":"+model)
		mu.Unlock()
		if id == "a" && model == "model-one" {
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, poolFrame("ok")+"data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Credentials: func() []Credential { return []Credential{poolCredential("a"), poolCredential("b")} }})
	for range 2 {
		out, err := c.Chat(context.Background(), ChatRequest{Model: "model-one", Prompt: "hi"}, nil)
		if err != nil || out.Text != "ok" {
			t.Fatalf("chat = %+v %v", out, err)
		}
	}
	c.poolMu.Lock()
	c.activeCredential = "a"
	c.poolMu.Unlock()
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "model-two"}, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(calls, ",") != "a:model-one,b:model-one,b:model-one,a:model-two" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestPoolStopsAfterDeliveredContentAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				fmt.Fprint(w, poolFrame("partial")+"data: invalid-json\n\n")
			}))
			defer srv.Close()
			c := New(Config{BaseURL: srv.URL, Credentials: func() []Credential { return []Credential{poolCredential("a"), poolCredential("b")} }})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := c.Chat(ctx, ChatRequest{Model: "m"}, func(StreamEvent) {
				if cancelled {
					cancel()
				}
			})
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestPoolConcurrentIdentityAndExplicitLogout(t *testing.T) {
	arrived := make(chan string, 3)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Cosy-User")
		if r.Header.Get("Cosy-Machineid") != "machine-"+id || r.Header.Get("Cosy-Key") != "key-"+id {
			t.Error("mixed credential identity")
		}
		arrived <- id
		<-release
		fmt.Fprint(w, poolFrame("ok"))
	}))
	defer srv.Close()
	accts := []Credential{poolCredential("a"), poolCredential("b")}
	c := New(Config{BaseURL: srv.URL, Credentials: func() []Credential { return accts }})
	errs := make(chan error, 3)
	for range 3 {
		go func() { _, err := c.Chat(context.Background(), ChatRequest{Model: "m"}, nil); errs <- err }()
	}
	count := map[string]int{}
	for range 3 {
		select {
		case id := <-arrived:
			count[id]++
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("pool stalled")
		}
	}
	close(release)
	for range 3 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if count["a"] != 2 || count["b"] != 1 {
		t.Fatalf("leases = %v", count)
	}
	accts = []Credential{}
	if _, err := c.loadCredential(); err == nil {
		t.Fatal("explicit logout fell back to IDE")
	}
	if _, err := c.Chat(context.Background(), ChatRequest{}, nil); err == nil {
		t.Fatal("logged-out chat succeeded")
	}
}

func TestPoolSkipsCoolingSlotsWithoutSpendingAttemptBudget(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; io.WriteString(w, poolFrame("ok")) }))
	defer srv.Close()
	var accts []Credential
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		accts = append(accts, poolCredential(id))
	}
	c := New(Config{BaseURL: srv.URL, Credentials: func() []Credential { return accts }})
	c.pool = map[string]*credentialSlot{}
	for _, a := range accts {
		c.pool[a.UserID] = &credentialSlot{credential: a, cool: map[string]time.Time{"m": time.Now().Add(time.Hour)}}
	}
	c.pool["e"].cool = map[string]time.Time{}
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d", hits)
	}
}
