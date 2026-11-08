package channels

import (
	"strings"
	"testing"

	"zenui/internal/zen/chprov"
)

// 渠道表是编译期清单，重复 id 会让面板出现两张同名卡片、让逐条排除键对不上。
func TestChannelIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Channels() {
		if seen[c.ID] {
			t.Fatalf("duplicate channel id %q", c.ID)
		}
		seen[c.ID] = true
	}
}

// 每一条都必须填齐面板要渲染的槽位：缺一个就是界面上一个空格子。
func TestChannelsAreFullySpecified(t *testing.T) {
	protocols := map[string]bool{ProtocolOpenAI: true, ProtocolAnthropic: true}
	authForms := map[string]bool{
		AuthBearer: true, AuthPasteKey: true, AuthOAuthDevice: true,
		AuthBrowserCallback: true, AuthSMS: true,
	}
	credits := map[string]bool{CreditPoints: true, CreditTokens: true, CreditNone: true}
	tiers := map[string]bool{chprov.TierExpiry: true, chprov.TierNever: true, chprov.TierNone: true}
	for _, c := range Channels() {
		switch {
		case c.ID == "", c.Name == "", c.Vendor == "":
			t.Errorf("%q: id/name/vendor 必须填齐", c.ID)
		case !protocols[c.Protocol]:
			t.Errorf("%s: 协议族 %q 未知", c.ID, c.Protocol)
		case !authForms[c.AuthForm]:
			t.Errorf("%s: 认证头形态 %q 未知", c.ID, c.AuthForm)
		case !credits[c.CreditUnit]:
			t.Errorf("%s: 计费单位 %q 未知", c.ID, c.CreditUnit)
		case !tiers[c.Tier]:
			t.Errorf("%s: 档位 %q 未知（面板会解释不出「为什么排最后」）", c.ID, c.Tier)
		case !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://"):
			t.Errorf("%s: 基址 %q 不是 URL", c.ID, c.BaseURL)
		case c.Note == "":
			t.Errorf("%s: 没有 Note，面板会显示一行空白", c.ID)
		}
	}
}

// 描述表里的每一家都必须有展示元数据。缺一条不会崩，只是面板上多一行 id ——
// 而那种「看起来填了、其实没填」的格子最难发现，故用断言挡住。
func TestEverySpecHasDisplayMetadata(t *testing.T) {
	for _, s := range chprov.All() {
		if _, ok := channelMeta[s.ID]; !ok {
			t.Errorf("chprov 里有 %q，channelMeta 里没有它的名字/厂商/协议族", s.ID)
		}
		if _, ok := baseURLOf[s.ID]; !ok {
			t.Errorf("%q 没有登记推理端点（面板会显示空白域名）", s.ID)
		}
	}
}

// 参考实现的 18 家必须都在：漏一家就等于把它的模型从聚合目录里静默摘掉。
func TestReferenceChannelsAreAllPresent(t *testing.T) {
	want := []string{
		"codearts", "buddy", "workbuddy", "lobsterai", "qoder", "qodercn",
		"trae", "cline", "loomy", "raccoon", "minimax", "minimaxintl",
		"zcode", "opencode", "gemini", "officeace", "accio", "qiniu",
	}
	for _, id := range want {
		if _, ok := ByID(id); !ok {
			t.Errorf("reference channel %q is missing", id)
		}
	}
}

// 本项目的 presets 也必须并进来（同名归一：longcat-anthropic 与 gemini 不重复登记）。
func TestPresetChannelsAreMerged(t *testing.T) {
	want := []string{
		"modelscope", "nvidia-nim", "zhipu", "siliconflow", "longcat", "groq",
		"openrouter", "github-models", "huggingface", "cerebras", "mistral", "anthropic",
	}
	for _, id := range want {
		if _, ok := ByID(id); !ok {
			t.Errorf("preset channel %q is missing", id)
		}
	}
	// 同名归一的两个反例：同一家的两个协议端点只算一家。
	if _, ok := ByID("longcat-anthropic"); ok {
		t.Errorf("longcat-anthropic 应合并进 longcat，而不是单独一条")
	}
}

// **全部 30 家都参与轮换** —— 这是本项目与参考实现的一处有意分歧。
//
// 参考实现只让「有临期折算」的 12 家参与，代价是用户在轮换列表里看不到其余那些家，
// 且**零报错零日志**（它为此记了七次同一个坑）。用户明确要求「那 27 个渠道也全部加
// 进去」，故这里反过来断言：一个都不能少。
//
// ⚠️ 这条断言的意义不是「数量对」，而是「没有任何一家被静默剥夺参与资格」。
func TestEveryChannelRotates(t *testing.T) {
	all := Channels()
	// 30 家 = 已接线的三家（workbuddy / qoder / qodercn）+ 用户点名要加进来的 27 家。
	if len(all) != 34 {
		t.Fatalf("渠道表有 %d 家，期望 34 家（含新增五家，QoderCN 兼容别名不重复计数）", len(all))
	}
	wired := WiredIDs()
	if len(wired) != len(all) {
		var missing []string
		for _, c := range all {
			if !c.ExpiryWired {
				missing = append(missing, c.ID)
			}
		}
		t.Fatalf("参不参与轮换的分叉：共 %d 家、只有 %d 家参与，缺 %v", len(all), len(wired), missing)
	}
	// 顺序即默认轮换序，须与描述表一致。
	for i, id := range wired {
		if id != all[i].ID {
			t.Fatalf("轮换序第 %d 位是 %q，渠道表第 %d 位是 %q", i, id, i, all[i].ID)
		}
	}
}

// 档位必须与描述表一致：面板照它解释「为什么这家排最后」。
func TestTierComesFromTheSpecTable(t *testing.T) {
	for _, c := range Channels() {
		spec, ok := chprov.SpecOf(c.ID)
		if !ok {
			t.Fatalf("%q 在 channels 表里但不在 chprov 描述表里", c.ID)
		}
		if c.Tier != spec.Tier {
			t.Fatalf("%q 的档位 %q 与描述表的 %q 不一致", c.ID, c.Tier, spec.Tier)
		}
	}
}

// 没登记的渠道一律不算参与：IsWired 的未知分支必须保守。
func TestIsWiredIsConservativeForUnknownIDs(t *testing.T) {
	if IsWired("not-a-channel") {
		t.Fatal("an unknown channel reported itself as wired")
	}
	if !IsWired("buddy") {
		t.Fatal("buddy 现在有临期来源（资源包 deductionEndTime），必须参与轮换")
	}
	if !IsWired("workbuddy") {
		t.Fatal("workbuddy reads real resource-package expiry and must rotate")
	}
}
