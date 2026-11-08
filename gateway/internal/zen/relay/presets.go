package relay

import (
	"regexp"
	"strings"

	"zenui/internal/zen/store"
)

// Preset is a curated free (or free-tier) upstream the dashboard offers as a
// one-click fill for the 自定义 API form. The user still supplies their own
// key — nothing here embeds credentials. Endpoints verified against the
// providers' docs and the community-maintained list
// github.com/cheahjs/free-llm-api-resources (2026-10).
//
// The free-tier shape is split into fields rather than one sentence: the
// dashboard lays every supplier out as the same four label/value rows, so a
// reader compares NVIDIA and ModelScope by scanning the same slots. Rank and
// Tier carry the ordering (tier 1 首选 → 3 备选); the number in Rank is the
// rank shown on the card.
type Preset struct {
	ID       string `json:"id"`
	Rank     int    `json:"rank"` // 1..N, the order the dashboard lists
	Tier     int    `json:"tier"` // 1 首选 / 2 可用 / 3 备选
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl"`
	Protocol string `json:"protocol"`
	Free     string `json:"free"`   // 免费档的形态：免费额度 / 免费档 / 付费
	Quota    string `json:"quota"`  // 额度或限速，写成"约 40 次/分"这种
	Region   string `json:"region"` // 海外 / 国内直连
	Models   string `json:"models"` // 免费档里实际能跑的东西
	Note     string `json:"note"`   // 接入要点（表单里的那行提示）
	KeyURL   string `json:"keyUrl"` // where to apply for the key
}

// Presets is the catalog the dashboard renders.
//
// The order is the ranking: 先能直接用、免费额度写得明白的，再要额外条件的，
// 最后是付费与实验性质的。同一 tier 内按"额度是否被写明"排——写明日额度或
// 每分钟速率的排在"按模型限速"之前。AMD is absent on purpose — AMD ships $100
// Developer Cloud GPU credits rather than a hosted free API, so there is no
// endpoint to point at.
var Presets = []Preset{
	{Tier: 1, ID: "modelscope", Name: "魔搭 ModelScope", BaseURL: "https://api-inference.modelscope.cn/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费推理", Quota: "每日 2000 次", Region: "国内直连",
		Models: "DeepSeek-V4-Pro、GLM-5.2、Qwen3.5-397B",
		Note:   "免费档里额度写得最明白的一家，旗舰模型也在其中。",
		KeyURL: "https://modelscope.cn/my/myaccesstoken"},
	{Tier: 1, ID: "nvidia-nim", Name: "NVIDIA NIM", BaseURL: "https://integrate.api.nvidia.com/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费额度", Quota: "约 40 次/分", Region: "海外",
		Models: "deepseek-ai/、qwen/、meta/、nvidia/ 前缀，几百个",
		Note:   "注册后在 build.nvidia.com 生成 Key，模型 ID 带厂商前缀。",
		KeyURL: "https://build.nvidia.com"},
	{Tier: 1, ID: "zhipu", Name: "智谱 BigModel", BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Protocol: store.ProtocolOpenAI,
		Free:     "Flash 系免费", Quota: "按模型限速", Region: "国内直连",
		Models: "GLM-4.5-Flash、GLM-4-Flash、GLM-4V-Flash",
		Note:   "只勾 Flash 系；同一家的 Pro 档要付费。",
		KeyURL: "https://open.bigmodel.cn/usercenter/apikeys"},
	{Tier: 1, ID: "siliconflow", Name: "硅基流动 SiliconFlow", BaseURL: "https://api.siliconflow.cn/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "常驻免费", Quota: "按模型限速", Region: "国内直连",
		Models: "Qwen3-8B / 14B / 32B、Qwen2.5-7B",
		Note:   "零余额的 Key 也能跑 Qwen 小杯；GLM-4-9B 与 Pro 档要付费。",
		KeyURL: "https://cloud.siliconflow.cn/account/ak"},
	{Tier: 1, ID: "longcat", Name: "LongCat（美团）", BaseURL: "https://api.longcat.chat/openai/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "注册即用", Quota: "未写明", Region: "国内直连",
		Models: "LongCat-Flash 系列，1M 上下文",
		Note:   "长上下文是它的特点，适合整份文档喂进去。",
		KeyURL: "https://longcat.chat"},
	{Tier: 2, ID: "gemini", Name: "Google AI Studio", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费", Quota: "按模型限速", Region: "海外",
		Models: "gemini-2.5-flash 等，日常够用",
		Note:   "Key 在 aistudio 生成；要能连到 googleapis。",
		KeyURL: "https://aistudio.google.com/apikey"},
	{Tier: 2, ID: "groq", Name: "Groq", BaseURL: "https://api.groq.com/openai/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费", Quota: "按模型限速", Region: "海外",
		Models: "llama-3.3-70b 等开源模型",
		Note:   "LPU 推理，首字最快的一家；模型面窄。",
		KeyURL: "https://console.groq.com/keys"},
	{Tier: 2, ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "聚合网关", Quota: ":free 后缀免费", Region: "海外",
		Models: "deepseek/deepseek-r1:free 等",
		Note:   "只有带 :free 后缀的模型免费，勾选时认后缀。",
		KeyURL: "https://openrouter.ai/keys"},
	{Tier: 2, ID: "github-models", Name: "GitHub Models", BaseURL: "https://models.github.ai/inference",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费", Quota: "按模型限速", Region: "海外",
		Models: "openai/gpt-4.1-mini 等，带命名空间",
		Note:   "GitHub PAT 直接当 Key，不用另外注册。",
		KeyURL: "https://github.com/settings/tokens"},
	{Tier: 2, ID: "huggingface", Name: "HuggingFace Router", BaseURL: "https://router.huggingface.co/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "每月赠送", Quota: "每月重置", Region: "海外",
		Models: "各家托管的开源模型",
		Note:   "额度每月重置；路由到第三方托管，速度看那一家。",
		KeyURL: "https://huggingface.co/settings/tokens"},
	{Tier: 2, ID: "cerebras", Name: "Cerebras", BaseURL: "https://api.cerebras.ai/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "免费", Quota: "按模型限速", Region: "海外",
		Models: "llama、qwen 开源模型",
		Note:   "晶圆级推理，速度和 Groq 一个量级。",
		KeyURL: "https://cloud.cerebras.ai"},
	{Tier: 2, ID: "longcat-anthropic", Name: "LongCat（Anthropic 协议）", BaseURL: "https://api.longcat.chat/anthropic",
		Protocol: store.ProtocolAnthropic,
		Free:     "注册即用", Quota: "同 OpenAI 端点", Region: "国内直连",
		Models: "同一批 LongCat-Flash 模型",
		Note:   "同一家换协议接入，喂给 Claude Code 这类只认 /messages 的客户端。",
		KeyURL: "https://longcat.chat"},
	{Tier: 3, ID: "mistral", Name: "Mistral", BaseURL: "https://api.mistral.ai/v1",
		Protocol: store.ProtocolOpenAI,
		Free:     "实验档", Quota: "1 次/秒", Region: "海外",
		Models: "mistral-large、codestral",
		Note:   "免费实验档限速 1 次/秒，适合试接，不适合跑量。",
		KeyURL: "https://console.mistral.ai/api-keys"},
	{Tier: 3, ID: "anthropic", Name: "Anthropic 官方", BaseURL: "https://api.anthropic.com/v1",
		Protocol: store.ProtocolAnthropic,
		Free:     "付费", Quota: "按 token 计费", Region: "海外",
		Models: "claude-opus-4-6 等",
		Note:   "没有免费档，列在这里是给 Anthropic 协议做个对照。",
		KeyURL: "https://console.anthropic.com/settings/keys"},
}

// Rank fills the ordinal each preset is shown with and keeps it in step with
// the slice order — the dashboard and the tests read Rank, not the index, so a
// single row can be moved without renumbering by hand.
func init() {
	for i := range Presets {
		Presets[i].Rank = i + 1
	}
}

// recommendPatterns lists, per provider, the model-id patterns worth enabling
// by default — curated from hands-on testing (NVIDIA free lane, Oct 2026:
// DeepSeek V4.1-Flash / Nemotron 3 Super / Nemotron 3 Ultra answer correctly
// at usable speeds; Kimi K3 and GLM-5.3 answer after 3-4 minute queues and are
// left out) and the provider's own free tier (Zhipu: the 4.x flash family).
var recommendPatterns = map[string][]string{
	"nvidia-nim": {
		"deepseek-ai/deepseek-v4.1-flash",
		"nvidia/nemotron-3-super",
		"nvidia/nemotron-3-ultra",
	},
	"integrate.api.nvidia.com": {
		"deepseek-ai/deepseek-v4.1-flash",
		"nvidia/nemotron-3-super",
		"nvidia/nemotron-3-ultra",
	},
	"zhipu": {
		"glm-4-flash", "glm-4.5-flash", "glm-4v-flash", "glm-4.1v-thinking-flash",
	},
	"open.bigmodel.cn": {
		"glm-4-flash", "glm-4.5-flash", "glm-4v-flash", "glm-4.1v-thinking-flash",
	},
	// SiliconFlow's free tier (verified Oct 2026 against a zero-balance key):
	// the Qwen small family answers for free; GLM-4-9B-0414, Hunyuan, Ling and
	// everything Pro/ are paid.
	"siliconflow": {
		"Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen3-8B", "Qwen/Qwen3-14B", "Qwen/Qwen3-32B",
	},
	"api.siliconflow.cn": {
		"Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen3-8B", "Qwen/Qwen3-14B", "Qwen/Qwen3-32B",
	},
	// ModelScope free inference (verified Oct 2026): the flagship tier answers
	// fast — DeepSeek-V4-Pro 1.5s, GLM-5.2 1.2s, Qwen3.5-397B 3.5s. MiniMax-M3
	// / ERNIE / GLM-4.7-Flash / LongCat-Lite returned empty deployments.
	"modelscope": {
		"deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4.1-Flash",
		"ZhipuAI/GLM-5.2", "Qwen/Qwen3.5-397B", "Qwen/Qwen3.5-122B",
		"Qwen/Qwen3.8-Flash-Next", "stepfun-ai/Step-3.7-Flash",
	},
	"api-inference.modelscope.cn": {
		"deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4.1-Flash",
		"ZhipuAI/GLM-5.2", "Qwen/Qwen3.5-397B", "Qwen/Qwen3.5-122B",
		"Qwen/Qwen3.8-Flash-Next", "stepfun-ai/Step-3.7-Flash",
	},
}

// junkRe filters non-chat entries (embedders, safety nets, parsers, …) that
// clutter big catalogs like NVIDIA NIM's.
var junkRe = regexp.MustCompile(`(?i)embed|rerank|retriev|guard|reward|parse|whisper|image|diffusion|ocr|safety|topic-control|moderation|tts|speech|transcri|clip`)

// smallRe filters sub-33B dense models and small MoE (太落后太低端的不要).
// Anchors keep "120b-a12b" and "a12b" from matching the 12b alternative.
var smallRe = regexp.MustCompile(`(?i)(^|[^a-z0-9])(1|2|3|4|6\.7|7|8|9|11|12|14|20|22|24|30|31|32)b([^a-z0-9]|$)`)

// oldRe filters 2023-2024 vintage families.
var oldRe = regexp.MustCompile(`(?i)llama-?2|codellama|chatqa|mixtral|codestral|mistral-7b|mistral-nemo|minitron|recurrentgemma|deepseek-coder|llama-3\.[12]|nemotron-4-340b|gemma-2\b`)

// RecommendedModels picks the model ids a provider should enable by default:
// a curated list for known providers (matched by preset id or base-URL host,
// with word-boundary matching so glm-4-flash never drags in paid glm-4-flashx),
// falling back to a junk/small/vintage heuristic for unknown catalogs. The
// result never contains ids outside the fetched list.
func RecommendedModels(presetID, baseURL string, models []string) []string {
	pats := recommendPatterns[strings.ToLower(strings.TrimSpace(presetID))]
	if pats == nil {
		if host := hostOf(baseURL); host != "" {
			pats = recommendPatterns[strings.ToLower(host)]
		}
	}
	if pats != nil {
		var out []string
		for _, m := range models {
			for _, pat := range pats {
				if matchPattern(m, pat) {
					out = append(out, m)
					break
				}
			}
		}
		if len(out) > 0 {
			return out
		}
		// curated list matched nothing (catalog rotated) → heuristic below
	}
	out := []string{}
	for _, m := range models {
		if junkRe.MatchString(m) || smallRe.MatchString(m) || oldRe.MatchString(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// matchPattern reports whether id equals pat or extends it on a word boundary
// ("glm-4-flash" matches "glm-4-flash-250414" but not "glm-4-flashx").
func matchPattern(id, pat string) bool {
	id, pat = strings.ToLower(strings.TrimSpace(id)), strings.ToLower(strings.TrimSpace(pat))
	if id == pat {
		return true
	}
	if !strings.HasPrefix(id, pat) || len(id) == len(pat) {
		return false
	}
	switch id[len(pat)] {
	case '-', '_', '.', ':', '@', '/':
		return true
	}
	return false
}

func hostOf(baseURL string) string {
	u := NormalizeBaseURL(baseURL)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}
