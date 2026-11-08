package aggregate

import (
	"errors"
	"math"
	"testing"
	"time"
)

func day(n int) time.Time {
	return time.Date(2026, 11, n, 12, 0, 0, 0, time.UTC)
}

// CanonicalKey 的归一矩阵：同一真实模型在各渠道的不同写法都必须落到同一个键，
// 而「看起来像同一家」的两个东西（-sg 区、-official 后缀、日期快照）必须**不**
// 合并 —— 错合并的代价是请求打到了不是用户要的模型，且没有任何提示。
func TestCanonicalKeyMatrix(t *testing.T) {
	cases := []struct {
		name             string
		channel, realID  string
		realName         string
		wantKey, wantVia string
		wantOK           bool
	}{
		// id 通道：剥厂商前缀、点号转连字符、去 :free、剥 sn-。
		{"裸 id", "buddy", "deepseek-v4.1-flash", "Deepseek-V4.1-Flash", "deepseek-v4-1-flash", "id", true},
		{"厂商前缀", "codearts", "deepseek-ai/DeepSeek-V4.1-Flash", "Deepseek-V4.1-Flash", "deepseek-v4-1-flash", "id", true},
		{"命名空间前缀与 :free", "openrouter", "cline-free/deepseek-r1:free", "", "deepseek-r1", "id", true},
		{"sn- 前缀（raccoon）", "raccoon", "sn-glm-5-3", "GLM-5-3 · x0.75", "glm-5-3", "id", true},
		{"不带 v 的版本号也要转", "buddy", "sensenova-6.8-flash", "", "sensenova-6-8-flash", "id", true},
		{"trae 的官方后缀不剥", "trae", "DeepSeek-V4-Flash-Official", "", "deepseek-v4-flash-official", "id", true},
		{"日期快照不剥", "buddy", "deepseek-v4-flash-0731", "", "deepseek-v4-flash-0731", "id", true},
		// name 通道：qoder 系的 id 是内部代号，只能靠展示名归组。
		{"qoder 走展示名", "qoder", "gfmodel", "GLM-5.3-Flash", "glm-5-3-flash", "name", true},
		{"qoder 展示名带倍率后缀", "qoder", "gfmodel", "GLM-5.3-Flash · x0.6", "glm-5-3-flash", "name", true},
		{"qoder 中国版同规则", "qodercn", "dfmodel", "DeepSeek-V4.1-Flash", "deepseek-v4-1-flash", "name", true},
		{"accio 展示名带空格", "accio", "1Drift", "Qwen 3.8 Flash", "qwen3-8-flash", "name", true},
		// 显式补丁：两个通道都算不对时才用。
		{"lobsterai 专有 id 补丁", "lobsterai", "deepseek-flash", "", "deepseek-v4-1-flash", "patch", true},
		{"accio 键形态补丁", "accio", "qwen-3-8-flash", "Qwen 3.8 Flash", "qwen3-8-flash", "patch", true},
		// 排除集：抽象路由别名不是真实模型。
		{"auto 别名", "qoder", "auto", "Auto", "", "", false},
		{"别名经展示名漏出也必须挡住", "qoder", "x-internal", "Auto", "", "", false},
		{"workbuddy 抽象路由", "workbuddy", "default-model", "Auto", "", "", false},
		{"空白 id 不进任何虚拟模型", "buddy", "   ", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, via, ok := CanonicalKey(tc.channel, tc.realID, tc.realName)
			if ok != tc.wantOK || key != tc.wantKey || via != tc.wantVia {
				t.Fatalf("CanonicalKey(%q,%q,%q) = %q,%q,%v; want %q,%q,%v",
					tc.channel, tc.realID, tc.realName, key, via, ok, tc.wantKey, tc.wantVia, tc.wantOK)
			}
		})
	}
}

// 展示名通道会错误合并 workbuddy 的 deepseek-v4.1-flash 与 deepseek-v4.1-flash-sg
// （两者展示名逐字相同，但 -sg 是新加坡区的另一条通道）。若让 name 通道优先或两
// 通道投票，这两个不同的东西会被并成一个虚拟模型，而请求只发往其中一个 ——
// 用户以为在用新加坡区，实际打到了主区。按渠道声明主通道就是为了这条。
func TestNameChannelDoesNotMergeAcrossRegions(t *testing.T) {
	mainKey, _, ok1 := CanonicalKey("workbuddy", "deepseek-v4.1-flash", "Deepseek-V4.1-Flash")
	sgKey, _, ok2 := CanonicalKey("workbuddy", "deepseek-v4.1-flash-sg", "Deepseek-V4.1-Flash")
	if !ok1 || !ok2 {
		t.Fatalf("both entries must map, got %v/%v", ok1, ok2)
	}
	if mainKey != "deepseek-v4-1-flash" || sgKey != "deepseek-v4-1-flash-sg" {
		t.Fatalf("keys = %q / %q, want the region suffix to keep them apart", mainKey, sgKey)
	}
}

// PriceFactor 的误判矩阵：名字里带 x 不等于倍率。这三条是参考实现审计实测的
// 真实错误形态，其中 Spark X2.5 那条在 loomy 上真实可达。
func TestPriceFactorMisjudgement(t *testing.T) {
	cases := []struct {
		name string
		want float64
	}{
		{"Spark X2.5", math.Inf(1)},
		{"GPT-X5", math.Inf(1)},
		{"Grok x4", math.Inf(1)},
		{"Hy3 X", math.Inf(1)},
		{"DeepSeek V4 Flash 0731 · x3.0", 3.0},
		{"GLM-5-3 · x0.75", 0.75},
		{"Qwen3.8-Flash ×4", 4.0},
		{"Kimi-K3 · x0.17→x0.50", 0.5},
		{"SenseNova-6.8-Flash 免费", 0},
		{"GLM-5 · 免费", 0},
		{"MiniMax-M3 · x99.9", 99.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PriceFactor(tc.name)
			if math.IsInf(tc.want, 1) {
				if !math.IsInf(got, 1) {
					t.Fatalf("PriceFactor(%q) = %v, want +Inf（不认成倍率）", tc.name, got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("PriceFactor(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func cand(channel, realID string, state string, expiry time.Time, price float64) Candidate {
	return Candidate{Channel: channel, RealID: realID, RealName: realID, State: state, Expiry: expiry, Price: price, Wired: true}
}

// Rank 的哨兵语义：不可用的剔除；永久（Never）参与排序但排在有临期的之后；
// 手工位次严格优先（即使它的临期更晚）；并列按倍率；全并列保持声明顺序。
func TestRankSentinelsAndOrder(t *testing.T) {
	entries := []Candidate{
		cand("buddy", "m", ExpiryNever, time.Time{}, math.Inf(1)),
		cand("codearts", "m", ExpiryKnown, day(3), 0.8),
		cand("loomy", "m", ExpiryUnusable, time.Time{}, 0.5),
		cand("zcode", "m", ExpiryKnown, day(1), 1.2),
		cand("trae", "m", ExpiryKnown, day(1), 0.7),
	}
	got, err := Rank(entries, nil)
	if err != nil {
		t.Fatalf("Rank returned error: %v", err)
	}
	want := []string{"trae", "zcode", "codearts", "buddy"}
	if len(got) != len(want) {
		t.Fatalf("kept %d candidates, want %d (%v)", len(got), len(want), got)
	}
	for i, ch := range want {
		if got[i].Channel != ch {
			names := []string{}
			for _, c := range got {
				names = append(names, c.Channel)
			}
			t.Fatalf("order = %v, want %v（不可用应剔除、临期升序、同档倍率升序、永久排最后）", names, want)
		}
	}
	// 纯函数：传入的切片不能被改写。
	for i, c := range entries {
		if c.Channel != entries[i].Channel {
			t.Fatalf("Rank reordered the caller's slice at %d", i)
		}
	}
}

// 手工位次是**严格序**：排过的候选排在全部未排过的之前，即使它临期更晚。
// 若临期仍优先，用户拖动会被临期值静默覆盖 —— 面板显示的顺序与真实轮换顺序分叉。
func TestRankManualRankBeatsExpiry(t *testing.T) {
	entries := []Candidate{
		cand("buddy", "m", ExpiryKnown, day(1), 1.0),
		cand("zcode", "m", ExpiryNever, time.Time{}, 1.0),
		cand("trae", "m", ExpiryKnown, day(2), 1.0),
	}
	manual := func(c Candidate) (int, bool) {
		if c.Channel == "zcode" {
			return 1, true
		}
		if c.Channel == "trae" {
			return 2, true
		}
		return 0, false
	}
	got, err := Rank(entries, manual)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Channel != "zcode" || got[1].Channel != "trae" || got[2].Channel != "buddy" {
		t.Fatalf("order = %s,%s,%s; want zcode,trae,buddy", got[0].Channel, got[1].Channel, got[2].Channel)
	}
}

// 两个永久候选之间禁止出现 NaN 比较（Inf-Inf）：它们的相对顺序必须是声明顺序，
// 不能让 sort 拿到不一致的比较结果。
func TestRankTwoNeversKeepDeclarationOrder(t *testing.T) {
	entries := []Candidate{
		cand("b-channel", "m", ExpiryNever, time.Time{}, math.Inf(1)),
		cand("a-channel", "m", ExpiryNever, time.Time{}, math.Inf(1)),
	}
	for i := 0; i < 20; i++ {
		got, err := Rank(entries, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Channel != "b-channel" || got[1].Channel != "a-channel" {
			t.Fatalf("run %d reordered two tied Nevers: %s,%s", i, got[0].Channel, got[1].Channel)
		}
	}
}

// PickSoonest 的两个边界：全不可用 = 真的没账号；全永久 = 账号都在但没有临期。
// 两者必须报不同的错误 —— 参考实现的真实缺陷就是把后者谎报成前者。
func TestPickSoonestSeparatesNoAccountFromPermanentOnly(t *testing.T) {
	all, err := PickSoonest([]Candidate{
		cand("buddy", "m", ExpiryUnusable, time.Time{}, 1),
		cand("trae", "m", ExpiryUnusable, time.Time{}, 1),
	})
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("all-unusable err = %v, want ErrNoCandidates", err)
	}
	if all.Channel != "" {
		t.Fatalf("all-unusable returned %q", all.Channel)
	}

	never, err := PickSoonest([]Candidate{
		cand("buddy", "m", ExpiryNever, time.Time{}, 1),
		cand("trae", "m", ExpiryNever, time.Time{}, 1),
	})
	if err != nil {
		t.Fatalf("all-permanent must still pick one, got err %v", err)
	}
	if never.Channel != "buddy" {
		t.Fatalf("all-permanent picked %q, want the first declared (buddy)", never.Channel)
	}

	if _, err := PickSoonest(nil); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("empty err = %v, want ErrNoCandidates", err)
	}
}

func TestPickSoonestPrefersSoonestAndKeepsTies(t *testing.T) {
	got, err := PickSoonest([]Candidate{
		cand("buddy", "m", ExpiryNever, time.Time{}, 1),
		cand("trae", "m", ExpiryKnown, day(5), 1),
		cand("zcode", "m", ExpiryKnown, day(2), 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != "zcode" {
		t.Fatalf("picked %q, want the one that expires soonest", got.Channel)
	}
	tie, err := PickSoonest([]Candidate{
		cand("buddy", "m", ExpiryKnown, day(2), 1),
		cand("trae", "m", ExpiryKnown, day(2), 1),
	})
	if err != nil || tie.Channel != "buddy" {
		t.Fatalf("tie = %q / %v, want the first declared", tie.Channel, err)
	}
}

// BuildVirtualModels 的排序是界面契约：候选数降序、同数按 key 升序；候选按渠道
// 名升序。界面靠这个顺序稳定，任何漂移都会让列表在刷新间跳动。
func TestBuildVirtualModelsOrderIsStable(t *testing.T) {
	catalogs := map[string][]Model{
		"zcode": {{ID: "glm-5.3", Name: "GLM-5.3 · x0.8"}},
		"buddy": {
			{ID: "deepseek-v4.1-flash", Name: "Deepseek-V4.1-Flash"},
			{ID: "glm-5.3", Name: "GLM-5.3"},
		},
		"codearts": {{ID: "deepseek-ai/DeepSeek-V4.1-Flash", Name: "DeepSeek V4.1 Flash"}},
		"loomy":    {{ID: " ", Name: ""}},
	}
	got := BuildVirtualModels(catalogs)
	if len(got) != 2 {
		t.Fatalf("built %d virtual models, want 2 (%+v)", len(got), got)
	}
	if got[0].Key != "deepseek-v4-1-flash" || len(got[0].Candidates) != 2 {
		t.Fatalf("first = %s with %d candidates, want the 2-candidate model first", got[0].Key, len(got[0].Candidates))
	}
	if got[0].Name != "DeepSeek V4.1 Flash" {
		t.Fatalf("display name = %q", got[0].Name)
	}
	if got[0].Candidates[0].Channel != "buddy" || got[0].Candidates[1].Channel != "codearts" {
		t.Fatalf("candidates not in channel order: %+v", got[0].Candidates)
	}
	if got[1].Key != "glm-5-3" || len(got[1].Candidates) != 2 {
		t.Fatalf("second = %s with %d candidates", got[1].Key, len(got[1].Candidates))
	}
	// 空白 id 必须被跳过：它会造出一个 name == "" 的退化虚拟模型。
	for _, vm := range got {
		if vm.Name == "" {
			t.Fatalf("a virtual model with an empty display name leaked through: %+v", vm)
		}
	}
}

func TestBuildVirtualModelsIsDeterministic(t *testing.T) {
	catalogs := map[string][]Model{
		"a": {{ID: "m-1"}, {ID: "m-2"}},
		"b": {{ID: "m-1"}, {ID: "m-3"}},
	}
	first := BuildVirtualModels(catalogs)
	for i := 0; i < 20; i++ {
		again := BuildVirtualModels(catalogs)
		for k := range first {
			if first[k].Key != again[k].Key {
				t.Fatalf("run %d drifted at %d: %s vs %s", i, k, first[k].Key, again[k].Key)
			}
		}
	}
}
