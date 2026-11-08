package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"zenui/internal/zen/aggregate"
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

type nativeRT func(*http.Request) (*http.Response, error)

func (f nativeRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func nativeResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func nativeTestAccount(id string) chprov.Account {
	return chprov.Account{ID: id, AccessToken: "jwt-" + id, Extra: map[string]string{"organization_code": "org-" + id, "api_key": "key-" + id, "api_key_expires_at": "1999999999000"}}
}

func TestNativeModelsHonorMembershipLogoutAndCredentialReplacement(t *testing.T) {
	s, _, st := newBareServer(t)
	a := nativeTestAccount("a")
	st.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.ChannelAccounts = map[string]chprov.Accounts{"magicrew": {a}}
	})
	s.checkinClient = &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/flows/models":
			return nativeResponse(r, 200, `{"code":1000,"data":{"models":[{"value":"glm-5.3","label":"GLM 5.3"},{"value":"paid-model","label":"Paid Model"}]}}`), nil
		case "/api/v1/products/model-points":
			return nativeResponse(r, 200, `{"code":1000,"data":[{"model_id":"glm-5.3","input_points":3000},{"model_id":"paid-model","input_points":5000}]}`), nil
		default:
			t.Fatal("unexpected catalog operation", r.URL.Path)
			return nil, nil
		}
	})}
	report := s.refreshNativeCatalogs(context.Background())
	if len(report.Failed) != 0 || len(s.nativeModels()["magicrew"]) != 2 {
		t.Fatal("full paid catalog not loaded", report)
	}
	virtuals := aggregate.BuildVirtualModels(s.aggCatalogs())
	expiries := map[string]lane.QuotaExpiry{"magicrew": {Never: true}}
	chain := s.aggregateChainFrom("aggregate/glm-5-3", st.Config(), expiries, virtuals)
	if len(chain) != 1 || chain[0].servedID() != "magicrew/glm-5.3" {
		t.Fatalf("unwired native candidate: %+v", chain)
	}
	st.Mutate(func(c *store.Config) { c.AggregateChannels = map[string]bool{"magicrew": false} })
	if len(s.aggregateChainFrom("aggregate/glm-5-3", st.Config(), expiries, virtuals)) != 0 {
		t.Fatal("disabled channel remained executable")
	}
	st.Mutate(func(c *store.Config) {
		c.AggregateChannels["magicrew"] = true
		c.AggregateExclude = []string{"magicrew|glm-5.3"}
	})
	if len(s.aggregateChainFrom("aggregate/glm-5-3", st.Config(), expiries, virtuals)) != 0 {
		t.Fatal("disabled model remained executable")
	}
	st.Mutate(func(c *store.Config) { c.ChannelAccounts["magicrew"][0].AccessToken = "replacement" })
	if len(s.nativeModels()["magicrew"]) != 0 {
		t.Fatal("replaced credentials inherited stale catalog")
	}
	st.Mutate(func(c *store.Config) { delete(c.ChannelAccounts, "magicrew") })
	if s.channelRoutable("magicrew") {
		t.Fatal("logout left a live native route")
	}
}

func TestNativeInferenceFailsOverOnlyBeforeContent(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty refusal", true: "partial output"}[partial], func(t *testing.T) {
			s, _, st := newBareServer(t)
			a, b := nativeTestAccount("a"), nativeTestAccount("b")
			st.Mutate(func(c *store.Config) {
				c.AggregateEnabled = true
				c.ChannelAccounts = map[string]chprov.Accounts{"magicrew": {a, b}}
			})
			model := chprov.NativeModel{ID: "glm-5.3", Name: "GLM 5.3"}
			s.nativeCatalog = map[string]nativeCatalogEntry{nativeCatalogKey("magicrew", a): {Models: []chprov.NativeModel{model}}, nativeCatalogKey("magicrew", b): {Models: []chprov.NativeModel{model}}}
			attempts := []string{}
			s.checkinClient = &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v1/quota/points/total" {
					return nativeResponse(r, 200, `{"code":1000,"data":{"total_points":50}}`), nil
				}
				if r.URL.Path != "/v1/chat/completions" {
					t.Fatal("unexpected native call", r.URL.Path)
				}
				key := r.Header.Get("Authorization")
				attempts = append(attempts, key)
				if strings.HasPrefix(key, "Bearer jwt-") {
					t.Fatal("management JWT leaked into inference")
				}
				if key == "Bearer key-a" {
					if partial {
						return nativeResponse(r, 200, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"), nil
					}
					return nativeResponse(r, 429, `{"error":{"message":"quota"}}`), nil
				}
				if r.Header.Get("organization-code") != "org-b" {
					t.Fatal("gateway key and organization mismatched")
				}
				return nativeResponse(r, 200, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"), nil
			})}
			text := ""
			out, err := s.nativeTurn(context.Background(), lane.Request{Messages: []lane.Message{{Role: lane.RoleUser, Parts: []lane.Part{lane.TextPart{Text: "hello"}}}}}, "magicrew", "glm-5.3", func(c lane.Chunk) {
				if c.Kind == lane.ChunkTextDelta {
					text += c.Delta
				}
			})
			if partial {
				if len(attempts) != 1 || err == nil || text != "first" {
					t.Fatal("partial stream replayed", attempts, text, err)
				}
			} else if len(attempts) != 2 || err != nil || out.ServedModel != "magicrew/glm-5.3" || text != "ok" {
				t.Fatal("account failover failed", attempts, text, err)
			}
		})
	}
}

func TestNativeIssuanceCannotResurrectLoggedOutAccount(t *testing.T) {
	s, _, st := newBareServer(t)
	a := nativeTestAccount("a")
	delete(a.Extra, "api_key")
	delete(a.Extra, "api_key_expires_at")
	st.Mutate(func(c *store.Config) { c.ChannelAccounts = map[string]chprov.Accounts{"magicrew": {a}} })
	s.checkinClient = &http.Client{Transport: nativeRT(func(r *http.Request) (*http.Response, error) {
		st.Mutate(func(c *store.Config) { delete(c.ChannelAccounts, "magicrew") })
		return nativeResponse(r, 200, `{"code":1000,"data":{"api_key":"new-secret","api_key_expires_at":1999999999000}}`), nil
	})}
	if _, err := s.prepareNative(context.Background(), "magicrew", a, true); err == nil {
		t.Fatal("late gateway key saved after logout")
	}
	if len(st.Config().ChannelAccounts["magicrew"]) != 0 {
		t.Fatal("account resurrected")
	}
}

func TestAstudioAccumulatedStreamConvertsTextReasonAndToolArguments(t *testing.T) {
	rewrite := astudioFrameRewriter()
	var text, reason, args string
	for _, frame := range []string{
		`{"id":"same","choices":[{"index":0,"delta":{"content":"你","reasoning_content":"想","tool_calls":[{"index":0,"function":{"arguments":"{\"a\":"}}]}}]}`,
		`{"id":"same","choices":[{"index":0,"delta":{"content":"你好","reasoning_content":"想完","tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}`,
		`{"id":"same","choices":[{"index":0,"delta":{"content":"你好","reasoning_content":"想完"},"finish_reason":"stop"}]}`,
	} {
		var root map[string]any
		if err := json.Unmarshal(rewrite([]byte(frame)), &root); err != nil {
			t.Fatal(err)
		}
		d := root["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		text += d["content"].(string)
		reason += d["reasoning_content"].(string)
		if calls, ok := d["tool_calls"].([]any); ok {
			args += calls[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
		}
	}
	if text != "你好" || reason != "想完" || args != `{"a":1}` {
		t.Fatal(text, reason, args)
	}
}
