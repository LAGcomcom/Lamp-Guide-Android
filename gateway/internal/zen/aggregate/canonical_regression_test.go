package aggregate

import "testing"

func TestCanonicalSpellingEquivalence(t *testing.T) {
	groups := [][]string{
		{"qwen3-8-flash", "Qwen 3.8 Flash", "vendor/Qwen-3.8-Flash:free", "qwen3_8_flash"},
		{"deepseek-v4-1-flash", "DeepSeek V 4.1 Flash", "deepseek-v4.1-flash-free"},
		{"glm-5-3", "GLM5.3", "sn-glm-5-3"},
		{"claude-sonnet-4-6", "Claude 4.6 Sonnet", "anthropic/claude-sonnet-4.6"},
		{"kimi-k2-8-preview", "Kimi K 2.8 Preview"},
	}
	for _, group := range groups {
		for _, spelling := range group {
			if got := NormalizeModelKey(spelling); got != group[0] {
				t.Errorf("%q => %q, want %q", spelling, got, group[0])
			}
		}
	}
}

func TestCanonicalKeepsModelVariantsDistinct(t *testing.T) {
	pairs := [][2]string{
		{"deepseek-v4.1-flash", "deepseek-v4-flash"},
		{"deepseek-v4-flash", "deepseek-v4-pro"},
		{"deepseek-v4-flash", "deepseek-v4-flash-0731"},
		{"deepseek-v4-flash", "deepseek-v4-flash-official"},
		{"deepseek-v4.1-flash", "deepseek-v4.1-flash-sg"},
		{"kimi-k2.7", "kimi-k2.7-code"},
		{"kimi-k2.7-code", "kimi-k2.7-code-highspeed"},
		{"qwen3.8-flash", "qwen3.8-flash-preview"},
		{"muse-spark-1.3", "muse-spark-1.3-contributor-free"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6-20261101"},
	}
	for _, pair := range pairs {
		if NormalizeModelKey(pair[0]) == NormalizeModelKey(pair[1]) {
			t.Errorf("different variants merged: %q / %q", pair[0], pair[1])
		}
	}
}

func TestVirtualModelsMergeSpellingsKeepRealCandidateIdentity(t *testing.T) {
	vms := BuildVirtualModels(map[string][]Model{
		"accio":    {{ID: "opaque-code", Name: "Qwen 3.8 Flash"}},
		"custom":   {{ID: "qwen3.8-flash"}, {ID: "qwen3.8-flash"}},
		"opencode": {{ID: "vendor/qwen-3.8-flash-free"}},
	})
	if len(vms) != 1 || vms[0].Key != "qwen3-8-flash" || len(vms[0].Candidates) != 3 {
		t.Fatalf("expected one model with three distinct candidates: %+v", vms)
	}
	if vms[0].Candidates[0].Key() != "accio|opaque-code" {
		t.Fatal("canonicalization must not change saved exclusion identities")
	}
}
