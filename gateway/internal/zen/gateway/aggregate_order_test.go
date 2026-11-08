package gateway

import (
	"testing"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// 渠道级顺序必须能排动**没有任何候选**的渠道。
//
// 这是实测踩出来的回归：30 家渠道里只有 2 家真的出现在某个虚拟模型的候选表里，
// 所以从候选表反推出来的位次表只有那 2 家。面板上拖动第 3 家往后的箭头时，
// 算出的名单里压根没有这些渠道 —— 位次写不进去、读不回来，重新渲染后又按临期
// 排序落回原位，用户看到的是「点了没反应」。
func TestChannelOrderMovesChannelsWithoutCandidates(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	// 把最后一家拖到最前：它不在任何虚拟模型的候选里，只有渠道级这一格能记它。
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateManualRank = map[string][]string{
			ChannelOrderKey: {"anthropic"},
		}
	})
	order := s.aggChannelOrder(s.Store.Config().AggregateManualRank, s.aggExpiries(true))
	if len(order) == 0 {
		t.Fatal("order 为空")
	}
	if order[0] != "anthropic" {
		t.Fatalf("order[0] = %v, want anthropic（渠道级手工位次即使没有候选也要生效）", order[0])
	}
}

// 渠道级顺序必须整段生效，而不是只把排在第一位的提到前面。
// 一次拖一整串（连续点几次箭头）时，中间那几步也要留住，否则界面显示的序
// 与真实序在第二次点击后就开始分叉。
func TestChannelOrderKeepsFullSequence(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateManualRank = map[string][]string{
			ChannelOrderKey: {"qoder", "workbuddy", "trae", "cline"},
		}
	})
	order := s.aggChannelOrder(s.Store.Config().AggregateManualRank, s.aggExpiries(true))
	want := []string{"qoder", "workbuddy", "trae", "cline"}
	for i, id := range want {
		if i >= len(order) || order[i] != id {
			t.Fatalf("order = %v, want 前四家 %v", order, want)
		}
	}
}

// 虚拟模型自己的名单仍然优先于渠道级顺序。
// 两者说的是两件事：「这个模型的退路先后」比「这家整体上先烧」更具体。
func TestVirtualRankBeatsChannelOrder(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{ID: "backup", Enabled: true, Protocol: store.ProtocolOpenAI, BaseURL: "http://127.0.0.1:1/v1", Models: []string{"glm-5.3-flash"}, ModelMeta: map[string]store.ModelMeta{"glm-5.3-flash": {Name: "GLM-5.3-Flash"}}})
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
		c.AggregateManualRank = map[string][]string{
			ChannelOrderKey: {"backup", "qoder"},
			"glm-5-3-flash": {"qoder", "backup"},
		}
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{
				"qoder":  {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()},
				"backup": {Known: true, ExpiryMS: time.Now().Add(72 * time.Hour).UnixMilli()},
			}
		},
		func(string) int { return 1 })
	chain := s.aggregateChain(aggregatePrefix + "glm-5-3-flash")
	if len(chain) < 2 {
		t.Fatalf("chain = %d 站", len(chain))
	}
	if chain[0].channel != "qoder" {
		t.Fatalf("chain[0] = %v, want qoder（虚拟模型自己的位次优先于渠道级）", chain[0].channel)
	}
}

func TestPartialModelOrderDoesNotBorrowAnotherModelsRank(t *testing.T) {
	manual := map[string][]string{"model-a": {"qoder"}, "model-b": {"backup"}, ChannelOrderKey: {"backup", "workbuddy", "qoder"}}
	if _, ok := aggregateManualPosition(manual, "model-a", "backup"); ok {
		t.Fatal("unranked candidate borrowed global or another model rank")
	}
	if rank, ok := aggregateManualPosition(manual, "model-a", "qoder"); !ok || rank != 0 {
		t.Fatal("model order ignored")
	}
	if rank, ok := aggregateManualPosition(manual, "model-c", "backup"); !ok || rank != 0 {
		t.Fatal("missing model order did not use channel order")
	}
}
