package channels

import (
	"strings"

	"zenui/internal/zen/chprov"
)

// 渠道表是**编译期清单**：每一步都由 chprov 的描述表派生，避免「两处各写一份」。
//
// # 两条纪律（都是参考实现用真缺陷换来的）
//
//  1. **决定候选表可见性的是「有没有额度来源」，不是「有没有归一化补丁」**。
//     只把渠道加进归一表、不加进额度来源，症状恒定**静默**：模型层看起来接好了，
//     候选层却被剔除，用户在轮换列表里**永远看不到**那一家，且零报错零日志。
//     本项目把这两件事写在**同一条记录**里（Channel.ExpiryWired + chprov.Spec），
//     没有「加了一处、忘了另一处」的缝。
//
//  2. **TierNone 不是「不参与」**。上游确实没有额度端点的那几家（qiniu / nvidia /
//     groq / …）按「永久额度」参与轮换：代价是耗尽后会被选中并失败一次；
//     代价更大的是把它们踢出轮换 —— 面板上有、候选表里永远轮不到、无日志。
//     每一家的档位都进面板，用户能看到「这家为什么排最后」。

// 协议族。取值与 store.ProtocolOpenAI / ProtocolAnthropic 一致，但这里**不复用**
// store 的常量：渠道表是编译期元数据，不该为了两个字符串把包依赖倒挂过来。
const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
)

// 认证头形态。这不是「登录流程」，而是「这个渠道的凭据从哪来、发请求时带什么头」
// —— 面板据此告诉用户该贴 Key 还是该去登录。逐家的**登录形态**在 chprov 表里。
const (
	AuthBearer          = "bearer"
	AuthPasteKey        = "paste-key"
	AuthOAuthDevice     = "oauth-device"
	AuthBrowserCallback = "browser-callback"
	AuthSMS             = "sms"
)

// 计费单位。CreditUnitNone 表示「拿不到额度信息」—— 这不是「免费」，别混用。
const (
	CreditPoints = "积分"
	CreditTokens = "token"
	CreditNone   = "无"
)

// Channel 是一条渠道元数据。
//
// ExpiryWired 与 Tier 是**唯一**两个有行为后果的字段：
// ExpiryWired=true 参与跨渠道轮换（本项目的 29 家全部为 true：上面第 2 条纪律）；
// Tier 说明它按哪一档折算（expiry / never / none），面板照此解释排序。
type Channel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	Protocol    string `json:"protocol"`
	AuthForm    string `json:"authForm"`
	BaseURL     string `json:"baseUrl"`
	ExpiryWired bool   `json:"wired"`
	// Tier 是参与资格档位（chprov.TierExpiry / TierNever / TierNone）。
	Tier       string `json:"tier"`
	CreditUnit string `json:"creditUnit"`
	DailyClaim bool   `json:"dailyClaim"`
	// VisionNote 记图片输入的**实测**结论：「支持」/「不支持」/「逐模型」。
	// 不写推测：参考实现里 accio 的 multimodal:false 模型**不报错、静默忽略图片**，
	// 把答案幻觉出来，故那一栏按逐模型声明。
	VisionNote string `json:"visionNote"`
	Note       string `json:"note"`
}

// channelMeta 是**展示层**元数据：名字、厂商、协议族、认证形态、计费单位、
// 签到能力、读图实测结论、面板上那句解释。行为字段（Tier / 额度端点 / 登录形态）
// 一律来自 chprov 描述表，本表不重复登记 —— 否则两处会漂移。
var channelMeta = map[string]Channel{
	"astudio":      {ID: "astudio", Name: "AStudio（讯飞）", Vendor: "讯飞", Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey, CreditUnit: CreditPoints, VisionNote: "以远端目录声明为准", Note: "从本机插件导入或填写自己的会话；每日奖励使用 Cookie，模型推理使用独立 Bearer Token。星火独立池不并入主余额。"},
	"autoclaw":     {ID: "autoclaw", Name: "AutoClaw（国内版）", Vendor: "智谱", Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey, CreditUnit: CreditPoints, VisionNote: "以远端目录声明为准", Note: "支持钱包查询与每日签到；钱包账本未对齐可用余额前，不推算积分到期。国内、国际账号分开保存。"},
	"autoclawintl": {ID: "autoclawintl", Name: "AutoClaw（国际版）", Vendor: "智谱", Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey, CreditUnit: CreditPoints, VisionNote: "以远端目录声明为准", Note: "国际站独立域名与凭据；支持钱包和每日签到。上游不返回领取数量时只显示领取状态。"},
	"magicrew":     {ID: "magicrew", Name: "超级麦吉（国内版）", Vendor: "MagiCrew", Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey, CreditUnit: CreditPoints, VisionNote: "以远端目录声明为准", Note: "网页登录 JWT 用于签到、余额和目录；模型网关独立签发 API Key。限时批次按北京时间到期。"},
	"magicrewintl": {ID: "magicrewintl", Name: "MagiCrew（国际版）", Vendor: "MagiCrew", Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey, CreditUnit: CreditPoints, VisionNote: "以远端目录声明为准", Note: "先在网页选中组织，再导入本机插件账号或填写 JWT 与组织码；国际站不得混用国内凭据。"},
	"workbuddy": {
		ID: "workbuddy", Name: "WorkBuddy（国际版）", Vendor: "腾讯",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "逐模型：deepseek-v4.1-flash 收图，hy3 / hy4-preview 不收",
		Note:       "计费资源包带 deductionEndTime。国际版没有签到接口（只有用量通知）。",
	},
	"qoder": {
		ID: "qoder", Name: "Qoder（中国版）", Vendor: "阿里系",
		Protocol: ProtocolOpenAI, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "以模型声明为准（is_vl 随目录下发）",
		Note:       "内置 QoderCN 转发支持多账号；qodercn 是兼容入口，共用一条渠道。仅在余额为正时参与轮换，永久哨兵折算为无到期。",
	},
	"qodercn": {
		ID: "qodercn", Name: "Qoder（中国版）", Vendor: "阿里系",
		Protocol: ProtocolOpenAI, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "以模型声明为准",
		Note:       "qoder 的兼容别名，展示与推理使用同一条渠道。",
	},
	"buddy": {
		ID: "buddy", Name: "CodeBuddy（腾讯）", Vendor: "腾讯",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "支持",
		Note:       "资源包带 deductionEndTime（与 workbuddy 同形）。state + 浏览器授权 + 轮询换 token。",
	},
	"codearts": {
		ID: "codearts", Name: "CodeArts（华为云）", Vendor: "华为云",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "以模型声明为准",
		Note:       "华为云 SDK-HMAC-SHA256 签名；报文顶层 end_date 是套餐周期结束日，只有基础积分包跟随它到期。",
	},
	"lobsterai": {
		ID: "lobsterai", Name: "LobsterAI（有道）", Vendor: "有道",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "不支持",
		Note:       "远端专有 id deepseek-flash 实际是 DeepSeek-V4.1-Flash。本机回环收 code 换 token。",
	},
	"trae": {
		ID: "trae", Name: "TRAE（字节跳动）", Vendor: "字节跳动",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "逐模型",
		Note:       "Cloud-IDE-JWT 头 + 自定义 SSE；三个身份 id（设备/市场/会话）都从 uid 派生，换账号必须换头。",
	},
	"cline": {
		ID: "cline", Name: "Cline", Vendor: "Cline",
		Protocol: ProtocolOpenAI, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "WorkOS 设备码；balance 是聚合标量（推测 micro-USD，无源码证据）⇒ 没有到期时刻，按永久参与。",
	},
	"loomy": {
		ID: "loomy", Name: "Loomy（讯飞）", Vendor: "讯飞",
		Protocol: ProtocolOpenAI, AuthForm: AuthSMS,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "唯一用短信/微信扫码登录且不能自动续期的渠道；每日赠送按自然日重置，是唯一允许折算「次日零点」的一家。",
	},
	"raccoon": {
		ID: "raccoon", Name: "Raccoon Work（商汤）", Vendor: "商汤",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "微信扫码 / 短信二选一；登录奖励一次性（幂等），不是每日签到。展示名里混着倍率（· x0.75），正是 PriceFactor 要剥的东西。",
	},
	"minimax": {
		ID: "minimax", Name: "MiniMax Code（中国版）", Vendor: "MiniMax",
		Protocol: ProtocolAnthropic, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "部分模型",
		Note:       "Anthropic Messages 协议族；每条 details[] 带 expire_at_ms（毫秒）⇒ 可精确折算。",
	},
	"minimaxintl": {
		ID: "minimaxintl", Name: "MiniMax Code（国际版）", Vendor: "MiniMax",
		Protocol: ProtocolAnthropic, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: true,
		VisionNote: "部分模型",
		Note:       "与中国版共用一份实现，只换 host。国际站按出口 IP 分流：不带 xff 时 token 不被认 ⇒ 折算恒失败。",
	},
	"zcode": {
		ID: "zcode", Name: "ZCode（智谱 z.ai）", Vendor: "智谱",
		Protocol: ProtocolAnthropic, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditTokens, DailyClaim: true,
		VisionNote: "不支持",
		Note:       "官方 CLI 设备式登录；data.balances[] 各带 expires_at。缺 Authorization 得 401，缺 X-Device-Mid 得 400 code 3001。",
	},
	"opencode": {
		ID: "opencode", Name: "OpenCode", Vendor: "OpenCode",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "目录来自 models.dev，模型 id 会透传远端 —— 归一化里唯一的注入面。余额是本地派生的通道可用性，零网络请求。",
	},
	"officeace": {
		ID: "officeace", Name: "OfficeAce（华为 MaaS）", Vendor: "华为云",
		Protocol: ProtocolOpenAI, AuthForm: AuthOAuthDevice,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "订阅的每个积分桶都带 expiresAt（领取自动，无手动签到）。读数必须与凭据绑定：凭据不在时报「未登录」。",
	},
	"accio": {
		ID: "accio", Name: "Accio（阿里）", Vendor: "阿里",
		Protocol: ProtocolOpenAI, AuthForm: AuthBrowserCallback,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "逐模型（multimodal:false 的模型会静默忽略图片并幻觉出答案）",
		Note:       "三端点传参方式各不相同：userinfo 必须 POST + body，其余 GET + query（用错即 405/400）。",
	},
	"qiniu": {
		ID: "qiniu", Name: "Qiniu（七牛云）", Vendor: "七牛云",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准（上游未做读图实测）",
		Note:       "上游没有任何额度端点：用量只在控制台可见。不是「还没做」⇒ 按永久参与。",
	},
	"gemini": {
		ID: "gemini", Name: "Gemini（Code Assist / AI Studio）", Vendor: "Google",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "Google OAuth + PKCE（回调 127.0.0.1:8845）；配额按模型分桶、没有到期字段 ⇒ 按永久参与。走代理才能连。",
	},
	"modelscope": {
		ID: "modelscope", Name: "魔搭 ModelScope", Vendor: "魔搭社区",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "免费档额度写得最明白（每日 2000 次），但没有下发「配额何时重置」⇒ 按永久参与，不猜零点。",
	},
	"nvidia-nim": {
		ID: "nvidia-nim", Name: "NVIDIA NIM", Vendor: "NVIDIA",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "免费额度按速率限制（约 40 次/分），没有会作废的池 ⇒ 按永久参与，落在最后。",
	},
	"zhipu": {
		ID: "zhipu", Name: "智谱 BigModel", Vendor: "智谱",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "只勾 Flash 系（Pro 档要付费）。与 zcode / minimax 同属智谱生态但不是同一条上游。",
	},
	"siliconflow": {
		ID: "siliconflow", Name: "硅基流动 SiliconFlow", Vendor: "硅基流动",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "零余额的 Key 也能跑 Qwen 小杯；常驻免费但按模型限速 ⇒ 按永久参与。",
	},
	"longcat": {
		ID: "longcat", Name: "LongCat（美团）", Vendor: "美团",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "OpenAI 与 Anthropic 两个端点是同一家的两种协议，合并成一条。注册即用、额度未写明。",
	},
	"groq": {
		ID: "groq", Name: "Groq", Vendor: "Groq",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "LPU 推理，首字最快的一家；按模型限速 ⇒ 没有临期池 ⇒ 按永久参与。",
	},
	"openrouter": {
		ID: "openrouter", Name: "OpenRouter", Vendor: "OpenRouter",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditPoints, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "只有带 :free 后缀的模型免费 —— 该后缀在归一化里会被剥掉。额度端点未实测 ⇒ 不登记。",
	},
	"github-models": {
		ID: "github-models", Name: "GitHub Models", Vendor: "GitHub",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "GitHub PAT 直接当 Key；按模型限速 ⇒ 按永久参与。",
	},
	"huggingface": {
		ID: "huggingface", Name: "HuggingFace Router", Vendor: "HuggingFace",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "额度每月重置，但重置时刻不随报文下发 ⇒ 按永久参与（与 modelscope 同因）。",
	},
	"cerebras": {
		ID: "cerebras", Name: "Cerebras", Vendor: "Cerebras",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "晶圆级推理，和 Groq 一个量级；按模型限速 ⇒ 按永久参与。",
	},
	"mistral": {
		ID: "mistral", Name: "Mistral", Vendor: "Mistral",
		Protocol: ProtocolOpenAI, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "以模型声明为准",
		Note:       "免费实验档限速 1 次/秒 ⇒ 没有临期概念 ⇒ 按永久参与。",
	},
	"anthropic": {
		ID: "anthropic", Name: "Anthropic 官方", Vendor: "Anthropic",
		Protocol: ProtocolAnthropic, AuthForm: AuthPasteKey,
		CreditUnit: CreditNone, DailyClaim: false,
		VisionNote: "支持",
		Note:       "付费、按 token 计费；列在这里是给 Anthropic 协议做个对照 ⇒ 按永久参与。",
	},
}

// baseURLOf 是**推理端点**（不是额度端点）：面板显示「这一家打的是哪个域名」，
// 用户据此判断要不要挂代理。额度端点在 chprov 描述表里（两者常常不是同一个 host，
// 例如 gemini 的推理在 generativelanguage、配额在 cloudcode-pa）。
var baseURLOf = map[string]string{
	"astudio":       "https://astronstudio-api-volces-prod.xf-yun.com/model-gateway/v1",
	"autoclaw":      "https://autoglm-acceleration-api.zhipuai.cn/autoclaw-proxy/proxy/autoclaw",
	"autoclawintl":  "https://autoglm-api.autoglm.ai/autoclaw-proxy/proxy/autoclaw",
	"magicrew":      "https://i-magic-service.letsmagic.cn/v1",
	"magicrewintl":  "https://magic-service.magicrew.ai/v1",
	"workbuddy":     "https://www.workbuddy.ai/v2",
	"qoder":         "http://127.0.0.1:8095/v1",
	"qodercn":       "http://127.0.0.1:8095/v1",
	"buddy":         "https://copilot.tencent.com/v2",
	"codearts":      "https://opengw.developer.huaweicloud.com/api/v1",
	"lobsterai":     "https://lobsterai-server.youdao.com",
	"trae":          "https://trae-api-cn.mchost.guru",
	"cline":         "https://api.cline.bot/api/v1",
	"loomy":         "https://loomyad.xunfei.cn/api/v1",
	"raccoon":       "https://xiaohuanxiong.com",
	"minimax":       "https://agent.minimax.cn/mavis/api/v1/llm/v1",
	"minimaxintl":   "https://agent.minimax.io/mavis/api/v1/llm/v1",
	"zcode":         "https://api.z.ai/api/anthropic/v1",
	"opencode":      "https://opencode.ai/inference/openai/v1",
	"officeace":     "https://officeace.cn-southwest-2.huaweicloud-agentarts.com/v2",
	"accio":         "https://phoenix-gw.alibaba.com",
	"qiniu":         "https://api.qnaigc.com/v1",
	"gemini":        "https://generativelanguage.googleapis.com/v1beta/openai",
	"modelscope":    "https://api-inference.modelscope.cn/v1",
	"nvidia-nim":    "https://integrate.api.nvidia.com/v1",
	"zhipu":         "https://open.bigmodel.cn/api/paas/v4",
	"siliconflow":   "https://api.siliconflow.cn/v1",
	"longcat":       "https://api.longcat.chat/openai/v1",
	"groq":          "https://api.groq.com/openai/v1",
	"openrouter":    "https://openrouter.ai/api/v1",
	"github-models": "https://models.github.ai/inference",
	"huggingface":   "https://router.huggingface.co/v1",
	"cerebras":      "https://api.cerebras.ai/v1",
	"mistral":       "https://api.mistral.ai/v1",
	"anthropic":     "https://api.anthropic.com/v1",
}

// Channels 是完整清单，**顺序即面板顺序**：先已接线的账号渠道，再其余。
//
// 它由 chprov 的描述表派生：每次调用重新拼一遍（29 条，成本可忽略），
// 换来的是「描述表加一家 ⇒ 面板自动多一行」，不存在漏登记某一面的缝。
func Channels() []Channel {
	specs := chprov.All()
	out := make([]Channel, 0, len(specs))
	for _, s := range specs {
		// Both legacy ids use the same CN auth, credential pool and forwarder.
		// Keep the alias accepted by ByID/SpecOf, but display and rank it once.
		if s.ID == "qodercn" {
			continue
		}
		meta, ok := channelMeta[s.ID]
		if !ok {
			// 描述表里有、展示表里没有：面板会渲染出一行 id 而不是名字。
			// 这不是「静默失败」而是可见的空格子，但**仍然**应该在测试里被挡住
			// （见 channels_test 的 TestEverySpecHasDisplayMetadata）。
			meta = Channel{ID: s.ID, Name: s.ID, Vendor: "未登记"}
		}
		meta.BaseURL = baseURLOf[s.ID]
		meta.Tier = s.Tier
		mode, _ := chprov.CheckinCapability(s.ID)
		meta.DailyClaim = mode == "automatic" || mode == "manual"
		// 29 家全部参与轮换：这是本项目与参考实现的一处**有意分歧**。
		// 参考实现只让「有临期折算」的 12 家参与；用户明确要求「那 27 个渠道也全部
		// 加进去」，因为「面板上有、候选表里永远轮不到」才是最难解释的状态。
		// 代价如实写进每一家的 note（TierNone 耗尽后会被选中并失败一次）。
		meta.ExpiryWired = s.Tier != ""
		out = append(out, meta)
	}
	return out
}

// ByID 找一条渠道元数据。
func ByID(id string) (Channel, bool) {
	id = strings.TrimSpace(id)
	if id == "qodercn" {
		id = "qoder"
	}
	for _, c := range Channels() {
		if c.ID == id {
			return c, true
		}
	}
	return Channel{}, false
}

// WiredIDs 是参与轮换的渠道 id（按表内顺序）。面板的「先烧谁」默认序就是它。
func WiredIDs() []string {
	out := []string{}
	for _, c := range Channels() {
		if c.ExpiryWired {
			out = append(out, c.ID)
		}
	}
	return out
}

// IsWired 报告某渠道是否参与轮换。未知渠道一律 false —— 「没登记」不等于「可用」。
func IsWired(id string) bool {
	c, ok := ByID(id)
	return ok && c.ExpiryWired
}
