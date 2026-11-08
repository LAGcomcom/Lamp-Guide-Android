package chprov

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 本文件是**编译期描述表**：29 家渠道各自「额度端点在哪、请求头长什么样、
// 响应里哪个字段是到期时刻、授权走哪条流程」。
//
// # 每一条都逐字对照参考实现（dsh-providers-hub 的 *-credits / *-auth）
//
// 端点与字段名**不是猜的**：它们来自那边逐渠道的探针笔记（该仓库把「实测 200」
// 与「未实测的组合」严格区分）。这里照抄端点，但**不照抄它的登录状态**——
// 一家的登录要不要走浏览器会话、要不要读客户端缓存，是两条独立事实。
//
// # 三档参与资格（Spec.Tier）
//
//	TierExpiry  报文里有真实到期时刻 → 参与排序，临期先烧
//	TierNever   只有余额、没有到期时刻 → 按「永久」参与，排在有临期之后
//	TierNone    上游**根本没有**额度端点 → 有账号即按「永久」参与
//
// ⚠️ TierNone 不是「不参与」。用户要的是「这一家也在轮换里」，而 TierNone 的
// 代价是**耗尽后会被选中并失败一次**（参考实现对 modelscope 明确接受了同一代价）。
// 把它踢出轮换的代价更大：面板上有这一家、候选表里永远轮不到，且零报错。
// 每一家的 Tier 都进面板，用户能看到「这家为什么排最后」。

// 参与资格档位。
const (
	TierExpiry = "expiry"
	TierNever  = "never"
	TierNone   = "none"
)

// AuthStyle 是额度请求带凭据的方式。**不是**登录流程。
type AuthStyle = string

const (
	AuthBearer     = "bearer"
	AuthQuery      = "query-token"
	AuthCookie     = "cookie"
	AuthHuaweiAKSK = "huawei-aksk"
	// AuthHuaweiV11 是华为 APIG 的**另一套**签名：V11-HMAC-SHA256（派生密钥 + 作用域）。
	//
	// ⚠️ 与 AuthHuaweiAKSK 不能互相顶替：同一台 officeace 网关上发 SDK-HMAC-SHA256
	// 得 400 `APIG.0624 Unsupported signature algorithm`，而发 V11 才 401（仅缺凭据）
	// —— 说明那个网关只认 V11。把两家压成一种签名，症状是「凭据明明有效、额度恒读不到」。
	AuthHuaweiV11 = "huawei-v11"
	AuthLoomySign = "loomy-sign"
	AuthCustom    = "custom"
)

// 授权怎么拿到。
const (
	LoginPaste  = "paste"
	LoginDevice = "device"
	LoginPKCE   = "pkce"
	LoginPoll   = "poll"
	LoginQR     = "qr"
	// LoginQoder 是 Qoder 自己的设备授权：授权页在 qoder.cn，换 token 在
	// openapi.qoder.com.cn，两者**不同域**；两段之间还要读一次 userinfo 才铸得出
	// cosy 凭据。它与 LoginPoll 外形相似但契约不同（poll 的 404 是「等用户」，
	// 不是「端点没了」），故单列一档，不复用通用实现。
	LoginQoder = "qoder-device"
	// LoginCliFlow 是「官方 CLI 的 init/poll 两段式设备授权」。
	//
	// ⚠️ 它与 LoginPoll 的外形相似（都是「发起 → 轮询」），但契约完全相反：
	//   - LoginPoll 由上游铸 state，请求体是 {}，响应里有 state + authUrl；
	//   - LoginCliFlow 由**本机**铸 pollToken（32 字节 hex = 64 字符），请求体里
	//     要 provider，响应给 flow_id + authorize_url + expires_at + poll_interval_sec。
	// 把两者压成一种形态，就是本项目踩过的那次「点了登录没反应」：同一段代码
	// 给 zcode 发了 {}，上游回 400 code 3001 parameter error，面板上只看到
	// 「发起登录失败」。
	LoginCliFlow = "cliflow"
	// LoginTicket 是华为 doer 的「本机回环收 secret → 轮询 ticket 换 AK/SK」。
	//
	// ⚠️ 它的两段**都在 GET 上**，且第一段不是 API 调用而是一个浏览器页面：
	// ticket 端点只挂 GET，带 ticket_id + secret 两个查询参数；没 secret 得
	// 「密钥不能为空」，POST 打它得 404 method POST not found（实测 GET/POST/PUT/
	// OPTIONS/HEAD/PATCH/DELETE 全试过）。本项目此前把它登记成 LoginPoll（POST + {}），
	// 于是面板上恒是「发起登录失败：HTTP 404」，而凭据其实完全正常 ——
	// 那是**形态登记错**，不是上游坏了。
	LoginTicket = "ticket"
	// LoginClaw 是华为 AgentArts 的「云端登记 state → 浏览器授权 → 轮询 code →
	// DPoP 换 STS」。officeace 走这条：它既不认票据流程，也不认普通 PKCE
	// （sts 端点缺 DPoP 头直接 400 Invalid header parameter: DPoP, required）。
	LoginClaw = "claw"
)

// QuotaSpec 是「怎么读出这家还剩多少、何时作废」。
type QuotaSpec struct {
	// BaseURL 与 Path 分开：面板要显示「打的是哪个域名」，测试要能整体换 host。
	BaseURL string
	Path    string
	Method  string
	Auth    AuthStyle
	// Headers 是这一家额外要求的头。nil 表示只要 Authorization。
	//
	// ⚠️ 写成函数而不是 map：trae 的头里有三个**从 uid 派生的 id**
	// （X-Device-Id / X-Market-User-Id / Vscode-Sessionid），zcode 要求
	// X-Device-Mid 来自凭据。写成静态 map 就只能提前算好，而提前算好意味着
	// 「换账号不换头」——那是把两套身份混成一个。
	Headers func(a Account) map[string]string
	// Body 是 POST 端点的请求体（空串表示 GET / 无 body）。
	Body string
	// DayReset 表示这份额度**按自然日重置**（loomy 的每日赠送）。
	// 只有它对应的那几家允许折算成「次日零点作废」。
	DayReset bool
}

// LoginSpec 是「授权怎么拿到」。
type LoginSpec struct {
	Kind LoginKind
	// 设备码：上游两个端点（发码 / 换 token）。
	DeviceURL string
	TokenURL  string
	// 设备码是否需要 client_id 与 scope（minimax 要，cline 走 workos 不要）。
	ClientID string
	Scope    string
	// PKCE 表示设备码请求必须带 code_challenge（S256）。
	//
	// ⚠️ 这不是可选项：minimax 两家不带时上游回 400
	// 「valid S256 PKCE is required」，而 panel 上看到的是「发起登录失败」——
	// 与「渠道没接上」同形。带上之后同一端点立刻回 device_code（已实测）。
	PKCE bool
	// PKCE：授权页与本机回调路径。
	AuthURL      string
	ExchangeURL  string
	CallbackPath string
	// 轮询：发起端点与查询端点（workbuddy / zcode 形态）。
	StartURL string
	PollURL  string
	// Provider 是 LoginCliFlow 里要写进请求体的 provider 名（zcode 要 "zai"）。
	//
	// ⚠️ 它是**上游的枚举值**，不是本项目的渠道 id：写成 spec.ID 会得
	// invalid_flow / 3004（已实测），而那个错误码与「端点没了」同形。
	Provider string
	// QR：取图端点。取图有两种形态，用 QRURL 的形状区分：
	//   - 含 {{uuid}} 占位：先请求它拿到 uuid，再把 uuid 拼成图 URL（raccoon）
	//   - 不含：请求它自己就是那张图（未用）
	// 微信扫码（loomy）走第三条：授权页 HTML 里内嵌 uuid，见 QRPageURL。
	QRURL string
	// QRPageURL 是「拿到 uuid 的那一页」（微信开放平台的 qrconnect 授权页）。
	// ⚠️ 它不是 JSON 端点：uuid 内嵌在 HTML 里（img src 与 fordevtool 两处），
	// 故这一形态必须用正则提，而不是读一个 uuid 字段（读字段恒为空 ⇒ 面板上
	// 恒是「上游没有给出二维码句柄」，而二维码其实就在那一页里）。
	QRPageURL string
	// QRAppID / QRRedirectURI 是微信授权页要求的两个参数（前者是公开 AppID）。
	QRAppID       string
	QRRedirectURI string
	// QRImageBase 是「uuid → 图片」的 URL 前缀（微信：/connect/qrcode/）。
	QRImageBase string
	// QRContent / QRImageURL 是「本机自己拼扫码内容」的形态（raccoon）：
	// code 由本机生成，二维码里装的是这一家的登录页 URL，图由本机渲染。
	QRContent string
	// --- ticket 形态（华为 doer）---
	// TicketBase 是 doer/redirect 基址；浏览器授权页是 AuthURL + ?service=<它>。
	TicketBase    string
	PluginName    string
	PluginVersion string
	// --- claw 形态（华为 AgentArts）---
	// StateURL 由上游登记 state（**必须用上游发的**），CodeURL 轮询换 code。
	StateURL string
	CodeURL  string
	// AuthorizeURL 是 IAM 的授权端点（authui/v1/oauth2/authorize），与 AuthURL
	// （登录页 authui/login.html）**不在同一路径层级**。
	//
	// ⚠️ 这两个不能拼接：登录页是 /authui/login.html，授权端点是
	// /authui/v1/oauth2/authorize。把 "AuthURL + /v1/oauth2/authorize" 拼起来
	// 会得到 /authui/login.html/v1/oauth2/authorize —— 上游回 404/400，面板上
	// 只看到「发起登录失败」，而那一条地址在浏览器里连登录页都打不开。
	AuthorizeURL string
	// RedirectURI 是云端回调地址（不是本机回环）。
	RedirectURI string
	// AuthQueryTemplate 是「授权页 query 按自家格式拼」的模板（loomy 除外的那几家）。
	//
	// 占位符：{{redirect_uri}} / {{state}} / {{client_id}} / {{scope}} /
	// {{code_challenge}} / {{code_challenge_method}}。空串表示走通用那套
	// （redirect_uri + state + response_type=code + PKCE 三件套）。
	//
	// ⚠️ 存在它的理由是「参数集合本身就是契约」：lobsterai 的登录页只认
	// source=electron + redirect_uri + state，多给一个 code_challenge 会被当作
	// 网页来源走另一条流程；trae/accio 又各自有必须带与必须不带的分野。
	AuthQueryTemplate string
	// Paste：这一家的 token 叫什么、去哪拿。
	PasteLabel string
	PasteHint  string
	Note       string
}

// LoginKind 是 LoginSpec.Kind 的类型，写成具名串以便面板直接渲染。
type LoginKind = string

// Spec 是一家渠道的完整描述。
type Spec struct {
	ID    string
	Tier  string
	Quota QuotaSpec
	Login LoginSpec
	// ExtraKeys 是这家凭据里除 token 之外必须带着的附加字段（device_mid / cookie / uid…）。
	// 面板据此提示「这家还要填什么」，额度读取据此判「凭据不全」。
	ExtraKeys []string
	Note      string
}

// --- 具体描述表 ---------------------------------------------------------------

// specs 是 29 家的描述表。workbuddy / qoder 两家走 lane 自己的实现（令牌轮换与
// sidecar 是本项目既有能力），这里登记它们是为了**面板只有一张表**：渠道的额度
// 来源与登录形态写在同一条记录里，不存在「加了一处、忘了另一处」的缝。
var specs = []Spec{
	newRewardSpec("astudio"),
	newRewardSpec("autoclaw"),
	newRewardSpec("autoclawintl"),
	newRewardSpec("magicrew"),
	newRewardSpec("magicrewintl"),
	{
		ID: "workbuddy", Tier: TierExpiry,
		// ⚠️ **POST + body {}**，不是 GET：实测 GET 得 `404 page not found`
		// （这条端点只挂了 POST 路由）。写成 GET 的症状是「凭据明明有效、额度恒读不到」，
		// 而 404 在面板上显示为「这次读不到额度」—— 看起来像网络问题。
		Quota: QuotaSpec{BaseURL: "https://www.workbuddy.ai", Path: "/v2/billing/meter/get-user-resource",
			Method: "POST", Auth: AuthBearer, Body: "{}",
			Headers: buddyHeaders("www.workbuddy.ai", "codebuddy", "WorkBuddy/5.5.2 WorkBuddy AI/5.5.2 CLI/5.5.2")},
		Login: LoginSpec{Kind: LoginPoll, StartURL: "https://www.workbuddy.ai/v2/plugin/auth/state?platform=CLI",
			PollURL: "https://www.workbuddy.ai/v2/plugin/auth/token",
			Note:    "上游铸一个 state 与 authUrl，用户在浏览器完成登录，这里轮询换 token。"},
		Note: "计费资源包带 deductionEndTime。国际版没有签到接口（只有用量通知）。",
	},
	{
		ID: "qoder", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "http://127.0.0.1:8095", Path: "/quota", Method: "GET", Auth: AuthCustom},
		// 内置设备授权：本机铸 verifier / challenge / nonce，授权页在 qoder.cn，
		// 换 token 在 openapi.qoder.com.cn。凭据落在**本表自己的**凭据槽里，
		// 与其余 28 家同形 —— 不再有「由网关托管、不在凭据表里」这一档。
		Login: LoginSpec{Kind: LoginQoder,
			AuthURL: "https://qoder.cn/users/sign-in",
			Note:    "浏览器授权：打开登录页并确认后，本机每 2 秒问一次授权结果；换到 token 后自动铸 cosy 凭据。"},
		ExtraKeys: []string{"cosy_key", "encrypt_user_info", "machine_id", "uid"},
		Note:      "两站共用同一份实现与同一份额度；上游 userQuota 的 expiresAt 是永久哨兵（9999-12-31），入口处折算成 Never。",
	},
	{
		ID: "qodercn", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "http://127.0.0.1:8095", Path: "/quota", Method: "GET", Auth: AuthCustom},
		Login: LoginSpec{Kind: LoginQoder,
			AuthURL: "https://qoder.cn/users/sign-in",
			Note:    "与 qoder 共用同一个实现与同一份额度，两条渠道各自折算。"},
		ExtraKeys: []string{"cosy_key", "encrypt_user_info", "machine_id", "uid"},
		Note:      "与 qoder 共用实现，但各占一行：漏登记中国版会让它单独被剔除，症状静默。",
	},
	{
		ID: "buddy", Tier: TierExpiry,
		// 同一族端点（POST + body {}），只换 host 与产品码。
		Quota: QuotaSpec{BaseURL: "https://copilot.tencent.com", Path: "/v2/billing/meter/get-user-resource",
			Method: "POST", Auth: AuthBearer, Body: "{}",
			Headers: buddyHeaders("copilot.tencent.com", "codebuddy", "CodeBuddyIDE/4.12.2")},
		Login: LoginSpec{Kind: LoginPoll, StartURL: "https://copilot.tencent.com/v2/plugin/auth/state?platform=CLI",
			PollURL:      "https://copilot.tencent.com/v2/plugin/auth/token",
			CallbackPath: "",
			Note:         "与 workbuddy 同一族的 OAuth：state + 浏览器授权 + 轮询换 token（CodeBuddy 产品线）。"},
		ExtraKeys: []string{"uid"},
		Note:      "资源包带 deductionEndTime（与 workbuddy 同形）。",
	},
	{
		ID: "codearts", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://snap-access.cn-north-4.myhuaweicloud.com",
			Path: "/snap-manager/v1/statistics/plugin", Method: "GET", Auth: AuthHuaweiAKSK,
			// ⚠️ 这两个头**签名之后**追加，绝不能进 canonical/SignedHeaders：
			// 实测带上签名回 401「verify ak sk signature fail」，签名后追加则 200。
			// 它们是服务端路由所需（Agent-Type 决定响应形态），不是凭据。
			//
			// ⚠️ GET 也不要 content-type：参考实现只在非 GET 时设它，
			// 多这一个头会让服务端重算的 canonical 多出一行。
			Headers: func(Account) map[string]string {
				return map[string]string{"Agent-Type": "PromptCenter", "X-Language": "zh-cn"}
			}},
		// ⚠️ 票据形态（不是 LoginPoll）：那一个 ticket 端点**只挂 GET**、且要两个
		// 查询参数（ticket_id + secret）。此前登记成 LoginPoll（POST + {}）得
		// 404 method POST not found，面板上就是「发起登录失败」。
		Login: LoginSpec{Kind: LoginTicket,
			TicketBase:    "https://devcloud.cn-north-4.huaweicloud.com/doer/redirect",
			AuthURL:       "https://auth.huaweicloud.com/authui/login.html",
			PollURL:       "https://snap-access.cn-north-4.myhuaweicloud.com/snap-manager/v1/login/ticket",
			CallbackPath:  "/authentication",
			PluginName:    "snap_jetbrains",
			PluginVersion: "26.3.3",
			Note: "华为云 doer 授权：点「登录」会在浏览器打开华为登录页，登录后它回本机一个 secret，" +
				"这里拿那个 secret 去换 AK/SK 与 securityToken，不用手抄任何东西。"},
		ExtraKeys: []string{"access_key_id", "secret_access_key", "security_token"},
		Note:      "报文顶层带 end_date（套餐周期结束日），只有基础积分包跟随它到期；签名不含 X-Snap-* 两个附加头，带上即验签失败。",
	},
	{
		ID: "lobsterai", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://lobsterai-server.youdao.com", Path: "/api/user/profile-summary",
			Method: "GET", Auth: AuthBearer,
			Headers: func(Account) map[string]string {
				return map[string]string{"accept": "application/json"}
			}},
		// ⚠️ 授权入口是 portal 的 **hash 路由**（/portal#/login），不是 /login：
		// 后者实测 404「页面不存在」，而前者 200 且会按 redirect_uri 回调。
		Login: LoginSpec{Kind: LoginPKCE, AuthURL: "https://lobsterai.youdao.com/portal#/login",
			ExchangeURL:  "https://lobsterai-server.youdao.com/api/auth/exchange",
			CallbackPath: "/auth/callback",
			// ⚠️ 这一家的 query 是**按自家格式**拼的，不能走通用那套（通用那套没有
			// source=electron，登录页会当成网页来源走另一条流程）。
			AuthQueryTemplate: "source=electron&redirect_uri={{redirect_uri}}&state={{state}}",
			Note:              "本机回环 127.0.0.1:{port}/auth/callback 收 code，再用它换访问令牌。"},
		Note: "远端专有 id deepseek-flash 实际是 DeepSeek-V4.1-Flash —— 归一补丁在 aggregate 包里登记着。",
	},
	{
		ID: "trae", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://api.trae.cn", Path: "/trae/api/v2/pay/ide_user_ent_usage",
			Method: "POST", Auth: AuthCustom, Body: "{\"require_usage\":true,\"req_source\":2}",
			Headers: func(a Account) map[string]string {
				uid := a.Field("uid")
				return map[string]string{
					"content-type":       "application/json",
					"accept":             "*/*",
					"accept-language":    "zh-CN",
					"user-agent":         "VSCode 1.107.1 (TRAE SOLO CN)",
					"authorization":      "Cloud-IDE-JWT " + a.AccessToken,
					"x-market-client-id": "VSCode 1.107.1",
					"x-market-user-id":   traeUUID(uid, "market"),
					"x-user-region":      "CN",
					"x-device-id":        traeDeviceID(uid),
					"x-tt-trace-id":      "00-" + traeHex(uid, "trace", 16) + "-01",
					"vscode-sessionid":   traeHex(uid, "session", 16),
					"x-request-id":       traeUUID(uid, "req"),
					"package-type":       "stable_cn",
					"x-lscbd-aid":        "787976",
					"x-lscbd-platform":   "windows",
					"app-version":        "1.107.1",
					"x-lgw-req-sdk-type": "3",
				}
			}},
		Login: LoginSpec{Kind: LoginPKCE, AuthURL: "https://www.trae.cn/authorization",
			ExchangeURL:  "https://api.trae.cn/cloudide/api/v3/trae/oauth/ExchangeToken",
			CallbackPath: "/authorize",
			Note:         "本机回环 :18080/authorize 收 refreshToken + userInfo + userJwt，再换 access_token。"},
		ExtraKeys: []string{"uid"},
		Note:      "chat 端点用 Cloud-IDE-JWT 头 + 自定义 SSE；三个身份 id 都从 uid 派生（换账号必须换头）。",
	},
	{
		ID: "cline", Tier: TierNever,
		Quota: QuotaSpec{BaseURL: "https://api.cline.bot/api/v1", Path: "/users/{{accountId}}/balance",
			Method: "GET", Auth: AuthCustom,
			Headers: func(a Account) map[string]string {
				return map[string]string{
					"accept":         "application/json",
					"http-referer":   "https://app.cline.bot",
					"x-title":        "Cline",
					"x-is-multiroot": "true",
					"x-client-type":  "vscode",
					"authorization":  "Bearer workos:" + a.AccessToken,
				}
			}},
		Login: LoginSpec{Kind: LoginDevice, DeviceURL: "https://api.workos.com/user_management/authorize/device",
			TokenURL: "https://api.workos.com/user_management/authenticate",
			// ⚠️ 这个 client id 是 Cline 自己的 WorkOS 应用：旧值
			// client_01J0FW6XN8N2XWN1RN5M8JVCEY 上游回 400 invalid_client「Unknown client」，
			// 面板上只看到「设备码请求失败」。
			ClientID: "client_01K3A541FN8TA3EPPHTD2325AR",
			Note:     "WorkOS 设备码：5 秒一轮、5 分钟过期（与参考实现同参数）。"},
		ExtraKeys: []string{"account_id"},
		Note: "balance 是聚合标量（推测 micro-USD，无源码证据）⇒ 没有到期时刻，按永久参与。" +
			"⚠️ Authorization 必须原样带 workos: 前缀，剥掉即 401。",
	},
	{
		ID: "loomy", Tier: TierExpiry,
		// ⚠️ 额度走**业务端点**，只认小写 token 头（不是 Authorization: Bearer，
		// 也不是账号端的 HMAC 签名）：少了它上游回 100002「缺少 token」，
		// 与「登录已失效」逐字同形。故这里用 AuthCustom + 显式头，
		// 不套 AuthLoomySign（那一条是**账号端点**的签名，业务端点不验它）。
		Quota: QuotaSpec{BaseURL: "https://loomyad.xunfei.cn/api/v1",
			Path: "/points/records?pageNo=1&pageSize=1&recordType=all", Method: "GET", Auth: AuthCustom,
			Headers: func(a Account) map[string]string {
				return map[string]string{"accept": "application/json", "token": a.AccessToken}
			},
			DayReset: true},
		// ⚠️ 微信扫码不是「请求一个 JSON 拿 uuid」：uuid 内嵌在授权页 HTML 里。
		// 读字段恒为空 ⇒ 面板上恒是「上游没有给出二维码句柄」。
		Login: LoginSpec{Kind: LoginQR,
			QRPageURL:     "https://open.weixin.qq.com/connect/qrconnect",
			QRAppID:       "wx18d60be432287cf8",
			QRRedirectURI: "https://loomy.xunfei.cn/oauth/wechat/callback",
			QRImageBase:   "https://open.weixin.qq.com/connect/qrcode/",
			Note:          "微信扫码（appid wx18d60be432287cf8）；登录后声明 14 天 session，没有 refresh 端点 ⇒ 过期只能重新扫码。"},
		ExtraKeys: []string{"session", "access_key", "secret_key"},
		Note: "两池：永久积分与每日赠送（dailyBalance 按自然日重置）⇒ 每日那一池按次日零点折算临期。" +
			"⚠️ 业务失败恒返回 HTTP 200，成败只读 body 的 code。",
	},
	{
		ID: "raccoon", Tier: TierNever,
		Quota: QuotaSpec{BaseURL: "https://xiaohuanxiong.com", Path: "/api/web/points/v1/balance",
			Method: "GET", Auth: AuthCustom,
			Headers: func(a Account) map[string]string {
				h := map[string]string{
					"accept":        "application/json",
					"content-type":  "application/json",
					"authorization": "Bearer " + a.AccessToken,
					// 个人账号这里是**空串**（客户端也照发这个头）：企业号才有组织码。
					"x-org-code":         a.Field("org"),
					"x-raccoon-language": "zh",
				}
				// 设备号：登录时随凭据落盘（换账号必须换它，否则上游把两个号看成同一台机器）。
				if v := a.Field("device_id"); v != "" {
					h["x-client-device-id"] = v
				}
				return h
			}},
		// ⚠️ raccoon 的扫码不需要上游给句柄：code 由**本机**生成（16 字节 hex），
		// 服务端只做轮询查询（任意自造 code 都被接受并进入 pending）。
		// QRURL 是**轮询**端点，二维码内容是这一家的登录页 URL，图由本机渲染成
		// data URL（不引第三方图床：那会让二维码依赖网络才能画出来）。
		Login: LoginSpec{Kind: LoginQR,
			QRURL:     "https://xiaohuanxiong.com/api/web/auth/v1/login_with_qrcode_code",
			QRContent: "https://xiaohuanxiong.com/login?code={{code}}&appname=" + url.QueryEscape("商汤小浣熊官网"),
			Note:      "微信扫码 / 短信二选一；登录奖励一次性（幂等），不是每日签到。"},
		ExtraKeys: []string{"device_id"},
		Note:      "available_points 是核心字段（缺即形状不对，不编造数字）；分项 reward/daily/monthly/topup 逐桶映射 ⇒ 没有到期时刻。",
	},
	{
		ID: "minimax", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://agent.minimax.cn", Path: "/minimax-cloud/api/v1/credit/details",
			Method: "GET", Auth: AuthBearer},
		Login: LoginSpec{Kind: LoginDevice, DeviceURL: "https://account.minimax.cn/oauth2/device/code",
			TokenURL: "https://account.minimax.cn/oauth2/token", ClientID: "mcode-public",
			Scope: "agent.default", PKCE: true,
			Note: "设备码 + PKCE；客户端 id 是公开的 mcode-public。"},
		Note: "每条 details[] 带 expire_at_ms（毫秒）⇒ 可精确折算，无已知代价。",
	},
	{
		ID: "minimaxintl", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://agent.minimax.io", Path: "/minimax-cloud/api/v1/credit/details",
			Method: "GET", Auth: AuthBearer},
		Login: LoginSpec{Kind: LoginDevice, DeviceURL: "https://account.minimax.io/oauth2/device/code",
			TokenURL: "https://account.minimax.io/oauth2/token", ClientID: "mcode-public",
			Scope: "agent.default", PKCE: true, Note: "与中国版同形，只换 host。"},
		Note: "⚠️ 国际站按出口 IP 分流：不带 xff 时 agent.minimax.io 307 到中国站、token 不被认 ⇒ 折算恒失败（与凭据失效同形）。",
	},
	{
		ID: "zcode", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://zcode.z.ai", Path: "/api/v1/zcode-plan/billing/balance",
			Method: "GET", Auth: AuthCustom,
			Headers: func(a Account) map[string]string {
				return map[string]string{
					"user-agent":           "ZCode/3.14.3",
					"http-referer":         "https://zcode.z.ai",
					"x-title":              "Z Code@electron",
					"x-zcode-app-version":  "3.14.3",
					"x-release-channel":    "production",
					"x-client-language":    "zh-CN",
					"x-client-timezone":    "Asia/Shanghai",
					"x-device-mid":         a.Field("device_mid"),
					"x-platform":           "win32",
					"x-os-category":        "windows",
					"x-zcode-agent":        "glm",
					"x-session-id":         zcodeSessionID(a.Field("device_mid")),
					"x-zcode-session-type": "other",
					"x-zcode-trace-id":     zcodeHex(a.Field("device_mid")),
					"anthropic-version":    "2023-06-01",
					"authorization":        "Bearer " + a.AccessToken,
				}
			}},
		Login: LoginSpec{Kind: LoginCliFlow, StartURL: "https://zcode.z.ai/api/v1/oauth/cli/init",
			PollURL: "https://zcode.z.ai/api/v1/oauth/cli/poll/", Provider: "zai",
			// 这句会原样出现在登录弹窗里，所以只写用户要做的事。
			//
			// 代码侧的约束记在这里：init 的 Bearer 必须是本机铸的 64 位 hex pollToken
			// （32 字符得 invalid_flow / 3004），body 必须有 provider（写 {} 得
			// parameter error / 3001）；交换回来的 jwt 才是额度端点认的那一份（zai 的
			// access token 打余额端点恒 401），而额度端点缺 x-device-mid 得 400 3001
			// —— 那一格由登录流自己铸（见 pollCliFlow）。
			Note: "走官方 CLI 的两段式设备授权：点「登录」会在浏览器打开 z.ai 授权页，登录并同意后，" +
				"这里自动换回令牌与设备标识，不用手抄任何东西。"},
		ExtraKeys: []string{"device_mid", "zai_access_token"},
		Note: "data.balances[] 各带 expires_at（秒）；企业版 displayMode=enterprise 不下发额度数字，此时按永久（不是 0）。" +
			"⚠️ 缺 Authorization 得 401，缺 X-Device-Mid 得 400 code 3001。",
	},
	{
		ID: "opencode", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "OpenCode Zen API Key",
			PasteHint: "opencode 的「余额」是**本地派生的通道可用性**（读账号池限速状态），零网络请求 ⇒ 没有额度端点。"},
		Note: "目录来自 models.dev，模型 id 会透传远端 —— 归一化里唯一的注入面。" +
			"⚠️ 上游无额度端点 ⇒ 有账号即按永久参与。",
	},
	{
		ID: "officeace", Tier: TierExpiry,
		// ⚠️ 额度端点是 /v1/subscription（**不是** /api/subscription/current）：后者
		// 只存在于 OfficeAce 客户端的**本机** 127.0.0.1:3004，云端打它得 404。
		// 而 /v1/subscription 在云端存在（不带凭据回 401 而不是 404）—— 404 会被
		// 当成「这家没有额度端点」，401 才能被当成「要登录」。
		Quota: QuotaSpec{BaseURL: "https://officeace.cn-southwest-2.huaweicloud-agentarts.com",
			Path: "/v1/subscription", Method: "GET", Auth: AuthHuaweiV11,
			Headers: func(Account) map[string]string { return map[string]string{"accept": "application/json"} }},
		Login: LoginSpec{Kind: LoginClaw,
			// ⚠️ 登录页与授权端点是两个不同路径，必须分开写：前者是
			// /authui/login.html，后者是 /authui/v1/oauth2/authorize。以前
			// 用 "AuthURL + /v1/oauth2/authorize" 拼出 /authui/login.html/v1/…，
			// 那条地址在浏览器里根本到不了授权端点。
			AuthURL:      "https://auth.huaweicloud.com/authui/login",
			AuthorizeURL: "https://auth.huaweicloud.com/authui/v1/oauth2/authorize",
			StateURL:     "https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/state",
			CodeURL:      "https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/code",
			ExchangeURL:  "https://sts.cn-north-4.myhuaweicloud.com/v1/oauth2/tokens",
			RedirectURI:  "https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/callback",
			ClientID:     "pdp5_for_agentarts",
			Scope:        "openid",
			Note:         "华为云 AgentArts：先在云端登记 state，浏览器里登录并同意后由云端收回 code，这里再换 STS 凭据。"},
		ExtraKeys: []string{"access_key_id", "secret_access_key", "security_token"},
		Note: "订阅的每个积分桶都带 expiresAt；领取是自动的，没有手动签到入口。" +
			"⚠️ 读数必须与凭据绑定：凭据不在时报「未登录」，不用陈旧缓存充数。",
	},
	{
		ID: "accio", Tier: TierExpiry,
		Quota: QuotaSpec{BaseURL: "https://phoenix-gw.alibaba.com", Path: "/api/entitlement/quota",
			Method: "GET", Auth: AuthQuery},
		Login: LoginSpec{Kind: LoginPKCE, AuthURL: "https://www.accio-ai.com/login",
			ExchangeURL:  "https://phoenix-gw.alibaba.com/api/oauth/token",
			CallbackPath: "/auth/callback",
			Note:         "PKCE + 本机回环；token 换发走 /api/oauth/token，刷新走 /api/auth/refresh_token。"},
		Note: "三端点传参方式**各不相同**：userinfo 必须 POST + body，其余 GET + query（用错即 405/400）。" +
			"⚠️ 图片通路逐模型：multimodal:false 的模型不报错、只是把图片忽略掉。",
	},
	{
		ID: "qiniu", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "七牛云 API Key",
			PasteHint: "用量只在控制台可见，上游**没有任何额度端点**（不是「还没做」）。"},
		Note: "上游确实没有额度端点 ⇒ 永不拿到到期时刻；按永久参与（耗尽后会被选中并失败一次）。",
	},
	{
		ID: "gemini", Tier: TierNever,
		Quota: QuotaSpec{BaseURL: "https://cloudcode-pa.googleapis.com", Path: "/v1internal:retrieveUserQuotaSummary",
			Method: "POST", Auth: AuthBearer},
		Login: LoginSpec{Kind: LoginPKCE, AuthURL: "https://accounts.google.com/o/oauth2/v2/auth",
			ExchangeURL:  "https://oauth2.googleapis.com/token",
			CallbackPath: "/oauth-callback",
			Note:         "Google OAuth + PKCE，回调 127.0.0.1:8845；scope 含 cloud-platform 与 userinfo。"},
		ExtraKeys: []string{"project"},
		Note: "配额按模型分桶、没有任何到期字段 ⇒ 按永久参与。" +
			"⚠️ 要走代理才能连（浏览器读系统代理、本进程不读）。",
	},
	{
		ID: "modelscope", Tier: TierNever,
		Quota: QuotaSpec{BaseURL: "https://www.modelscope.cn", Path: "/openapi/v1/magicubes/balance",
			Method: "GET", Auth: AuthBearer,
			Headers: func(Account) map[string]string {
				return map[string]string{"accept": "application/json", "referer": "https://www.modelscope.cn/my/overview"}
			}},
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "魔搭访问令牌",
			PasteHint: "modelscope.cn/my/myaccesstoken 生成；余额按魔粒计，报文里没有任何时间字段。"},
		Note: "免费档额度写得最明白（每日 2000 次），但**没有**下发「配额何时重置」⇒ 按永久参与，不猜零点。" +
			"⚠️ 耗尽后仍会被选中并失败一次（参考实现明确接受了这个代价）。",
	},
	{
		ID: "nvidia-nim", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "NVIDIA API Key",
			PasteHint: "build.nvidia.com 生成；免费额度按**速率**限制（约 40 次/分），没有会作废的池。"},
		Note: "按速率限制 ⇒ 没有到期池可排 ⇒ 按永久参与（落在最后）。",
	},
	{
		ID: "zhipu", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "智谱 API Key",
			PasteHint: "open.bigmodel.cn 用户中心生成；只勾 Flash 系，Pro 档要付费。"},
		Note: "按模型限速、报文无到期时刻 ⇒ 按永久参与。与 zcode / minimax 同属智谱生态但不是同一条上游。",
	},
	{
		ID: "siliconflow", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "硅基流动 API Key",
			PasteHint: "cloud.siliconflow.cn 生成；零余额的 Key 也能跑 Qwen 小杯。"},
		Note: "常驻免费但按模型限速 ⇒ 无临期 ⇒ 按永久参与。",
	},
	{
		ID: "longcat", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "LongCat API Key",
			PasteHint: "longcat.chat 注册即用；额度未写明。Anthropic 端点与 OpenAI 端点是同一家的两种协议。"},
		Note: "注册即用、额度未写明 ⇒ 按永久参与。",
	},
	{
		ID: "groq", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "Groq API Key",
			PasteHint: "console.groq.com/keys 生成；LPU 推理，首字最快的一家。"},
		Note: "按模型限速 ⇒ 没有临期池 ⇒ 按永久参与。",
	},
	{
		ID: "openrouter", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "OpenRouter API Key",
			PasteHint: "openrouter.ai/keys 生成；只有带 :free 后缀的模型免费 —— 该后缀在归一化里会被剥掉。"},
		Note: "额度端点本仓库未实测 ⇒ 不登记（宁可少读一次，不拿未实测的字段去排序）⇒ 按永久参与。",
	},
	{
		ID: "github-models", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "GitHub PAT",
			PasteHint: "github.com/settings/tokens 生成细粒度 PAT，直接当 Key。"},
		Note: "按模型限速 ⇒ 无临期 ⇒ 按永久参与。",
	},
	{
		ID: "huggingface", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "HuggingFace Token",
			PasteHint: "huggingface.co/settings/tokens 生成；Router 端点按每月重置，但重置时刻不下发。"},
		Note: "额度每月重置、重置时刻不随报文下发 ⇒ 按永久参与（与 modelscope 同因）。",
	},
	{
		ID: "cerebras", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "Cerebras API Key",
			PasteHint: "cloud.cerebras.ai 生成；晶圆级推理，和 Groq 一个量级。"},
		Note: "按模型限速 ⇒ 无临期 ⇒ 按永久参与。",
	},
	{
		ID: "mistral", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "Mistral API Key",
			PasteHint: "console.mistral.ai/api-keys 生成；免费实验档限速 1 次/秒。"},
		Note: "免费实验档限速 ⇒ 没有临期概念 ⇒ 按永久参与。",
	},
	{
		ID: "anthropic", Tier: TierNone,
		Login: LoginSpec{Kind: LoginPaste, PasteLabel: "Anthropic API Key",
			PasteHint: "console.anthropic.com 生成；付费、按 token 计费。"},
		Note: "付费按 token 计费，列在这里给 Anthropic 协议做对照 ⇒ 按永久参与。",
	},
}

// SpecOf 找一条描述。
func SpecOf(id string) (Spec, bool) {
	id = strings.TrimSpace(id)
	for _, s := range specs {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// buddyHeaders 是腾讯 buddy 系（workbuddy / CodeBuddy）额度端点的请求头。
//
// ⚠️ 三件事都不能省：
//
//	X-Domain       网关按它决定把请求归到哪个区域；缺它会被当成别的产品
//	X-Product      "SaaS"（官方客户端恒发这个值，不是猜的）
//	X-Product-Code 产品码，两家各自不同
//
// 少了任何一个都不会报「缺头」，而是返回一个看起来像业务错误的响应 ——
// 排查时会被误判成「凭据失效」。
func buddyHeaders(domain, productCode, ua string) func(Account) map[string]string {
	return func(a Account) map[string]string {
		h := map[string]string{
			"accept":         "application/json",
			"content-type":   "application/json",
			"user-agent":     ua,
			"x-domain":       domain,
			"x-product":      "SaaS",
			"x-product-code": productCode,
		}
		if uid := a.Field("uid"); uid != "" {
			h["x-user-id"] = uid
		}
		if ent := a.Field("enterprise_id"); ent != "" {
			h["x-enterprise-id"] = ent
			h["x-tenant-id"] = ent
		}
		return h
	}
}

// QuotaExtraKeys 是「读这家额度时**真的会用到**的附加字段」。
//
// ⚠️ 它比 Spec.ExtraKeys 窄：后者是「这家凭据整体要带什么」（面板据此提示），
// 里面混着 uid / project / account_id 这类由令牌或后端派生的字段 —— 缺它们不影响
// 读额度，拿整表去校验会把能用的凭据判成「不完整」。
func QuotaExtraKeys(id string) []string {
	switch id {
	case "astudio":
		return []string{"sso_session_id"}
	case "magicrewintl":
		return []string{"organization_code"}
	case "codearts":
		return []string{"access_key_id", "secret_access_key"}
	case "loomy":
		return []string{"access_key", "secret_key"}
	case "officeace":
		return []string{"cookie"}
	case "zcode":
		return []string{"device_mid"}
	}
	return nil
}

// MustSpec 是 SpecOf 的「已确认存在」版本：调用方刚用 SpecOf 判过存在性时用它，
// 免得再写一次 if 分支。不存在时返回一条以 id 为名的空描述（调用方只读 Note/Quota，
// 不会因此拿到别人的端点）。
func MustSpec(id string) Spec {
	if s, ok := SpecOf(id); ok {
		return s
	}
	return Spec{ID: id}
}

// All 返回全部描述（面板与测试共用同一份）。
func All() []Spec { return specs }

// IDs 是描述表里的 id，按字典序返回。
func IDs() []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.ID)
	}
	sort.Strings(out)
	return out
}

// URL 拼出额度端点的完整地址，并把 {{...}} 占位替换成凭据里的字段。
func (s QuotaSpec) URL(a Account) string {
	path := s.Path
	for _, k := range []string{"accountId", "uid", "project", "org", "account_id"} {
		key := "{{" + k + "}}"
		if !strings.Contains(path, key) {
			continue
		}
		v := a.Field(k)
		path = strings.ReplaceAll(path, key, url.PathEscape(v))
	}
	if s.Auth == AuthQuery {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return strings.TrimRight(s.BaseURL, "/") + path + sep + "accessToken=" + url.QueryEscape(a.AccessToken)
	}
	return strings.TrimRight(s.BaseURL, "/") + path
}

// --- 派生辅助（trae / zcode 的稳定身份 id）------------------------------------
//
// 两家都要求「同一账号永远呈现同一台机器」。参考实现的做法是从 uid 派生：
// 换账号必须换头，否则上游会把两个账号看成同一台机器 —— 那正是被封的形状。

func seededBytes(seed, salt string, n int) []byte {
	out := make([]byte, 0, n)
	for i := 0; len(out) < n; i++ {
		h := sha256.Sum256([]byte(seed + "|" + salt + "|" + strconv.Itoa(i)))
		out = append(out, h[:]...)
	}
	return out[:n]
}

func seededDigits(n int, seed, salt string) string {
	bs := seededBytes(seed, salt, n)
	var b strings.Builder
	for _, v := range bs {
		b.WriteByte('0' + v%10)
	}
	return b.String()
}

// traeDeviceID 是 15 位数字设备号。
func traeDeviceID(uid string) string { return seededDigits(15, uid, "devid") }

// traeHex 是 n 位十六进制串。
func traeHex(uid, salt string, n int) string {
	bs := seededBytes(uid, salt, n)
	var b strings.Builder
	for _, v := range bs {
		b.WriteString(hex.EncodeToString([]byte{v}))
	}
	return b.String()[:n]
}

// traeUUID 是确定性 UUID v4（与参考实现的 deriveMarketUserId 同形）。
func traeUUID(uid, salt string) string {
	bs := seededBytes(uid, salt, 16)
	bs[6] = (bs[6] & 0x0F) | 0x40
	bs[8] = (bs[8] & 0x3F) | 0x80
	h := hex.EncodeToString(bs)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// zcodeSessionID 是 8 位十六进制，按 device_mid 派生（同一设备稳定）。
func zcodeSessionID(mid string) string {
	h := zcodeHex(mid)
	if len(h) < 8 {
		return h
	}
	return h[:8]
}

// zcodeHex 是 md5(device_mid) 的十六进制串。
func zcodeHex(mid string) string {
	sum := md5.Sum([]byte(mid))
	return hex.EncodeToString(sum[:])
}

// NextDayStartMS 是「这一天的额度到今天 24:00 作废」的口径（loomy 的每日赠送）。
//
// ⚠️ 只服务**明确按自然日重置**的额度（QuotaSpec.DayReset）。别拿它去填其它
// 渠道的未知时刻 —— 那是凭空造一个上游从未承诺的时间。
func NextDayStartMS(now time.Time) int64 {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()).UnixMilli()
}
