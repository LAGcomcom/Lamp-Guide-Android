package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"zenui/internal/zen/channels"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// 429 里的 suggestions 是用户下一步真会去点的东西，所以它必须：
//   1. 全是自己能走通的 id（不列一个「选了也没用」的名字）；
//   2. 聚合关着时不出现 aggregate/*（那时它选了也没用）；
//   3. 请求本身是 aggregate/* 时不再推荐 aggregate/*（让客户端自己绕圈）。

func aggEnabledServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			out := map[string]lane.QuotaExpiry{}
			for _, id := range channels.WiredIDs() {
				out[id] = lane.QuotaExpiry{Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()}
			}
			return out
		},
		func(string) int { return 1 })
	return s
}

func TestSuggestionsCarryAggregateEntriesWhenOn(t *testing.T) {
	s := aggEnabledServer(t)
	out := s.failoverSuggestions("some-model", lane.Needs{}, "some-model", 3)
	joined := strings.Join(out, ",")
	if !strings.Contains(joined, "aggregate/auto") {
		t.Fatalf("suggestions = %v, want aggregate/auto", out)
	}
	if !strings.Contains(joined, "aggregate/glm-5-3-flash") {
		t.Fatalf("suggestions = %v, want the ready canonical name", out)
	}
	// 去重：同一个 id 出现两次会让客户端选择器多一条假选项。
	seen := map[string]bool{}
	for _, id := range out {
		if seen[id] {
			t.Fatalf("suggestions 里 %q 出现两次: %v", id, out)
		}
		seen[id] = true
	}
}

func TestSuggestionsSkipAggregateWhenOff(t *testing.T) {
	s := aggEnabledServer(t)
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = false })
	for _, id := range s.failoverSuggestions("some-model", lane.Needs{}, "some-model", 3) {
		if strings.HasPrefix(id, "aggregate/") {
			t.Fatalf("聚合关着却推荐了 %q", id)
		}
	}
}

func TestSuggestionsDoNotSelfReferenceOnAggregateRequest(t *testing.T) {
	s := aggEnabledServer(t)
	for _, id := range s.failoverSuggestions("aggregate/auto", lane.Needs{}, "aggregate/auto", 3) {
		if strings.HasPrefix(id, "aggregate/") {
			t.Fatalf("请求本身就是聚合名，却又推荐了 %q", id)
		}
	}
}

// 429 错误体那条路径：建议必须真的写进 error.suggestions，而不是只算出来放在
// 变量里。这条不打真实上游 —— lane 的限流判定属于 lane 自己的测试（它对上游
// 失败形态有严格分类），这里只钉住「算出来的东西有没有送出去」。
func TestQuotaErrorBodyCarriesAggregateSuggestion(t *testing.T) {
	s := aggEnabledServer(t)
	sugg := s.failoverSuggestions("mimo-v2.6-flash-free", lane.Needs{}, "mimo-v2.6-flash-free", 3)
	body := withSuggestions(openaiError("上游限流", "rate_limit_error"), sugg)
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "aggregate/") {
		t.Fatalf("错误体里没有聚合建议: %s", payload)
	}
	var back struct {
		Error struct {
			Suggestions []string `json:"suggestions"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Error.Suggestions) == 0 {
		t.Fatal("suggestions 是空的")
	}
}
