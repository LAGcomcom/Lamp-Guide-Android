// Package aggregate 是「跨渠道聚合选型」的纯函数层：把同一个真实模型在各渠道
// 不同 id / 不同展示名归到一个规范键，按「最快作废的额度先用」排出候选链。
//
// # 为什么单独成包、且完全不碰 IO
//
// 选型口径必须是**唯一权威**。参考实现（dsh-providers-hub 的 aggregate-core /
// aggregate-catalog / canonical-models）刻意把这一层抽出来，理由写在那边
// aggregate-core 的模块注释里：同一语义若在两处实现（auto 与新 provider 各一份）
// 必然漂移，症状是「同一个档位在两处算出不同结果」，而那种缺陷**测试全绿**、
// 只在畸形响应上分叉 —— 静默失效。这里沿用同一条纪律：本包不做网络、不读
// store、不读时钟，全部输入都是参数，全部结论都可断言。
//
// # 哨兵语义（三者严格互斥，绝不合成一个变量）
//
//	状态           含义                              排序行为
//	ExpiryNever    可用，但只有永久/长期积分        参与排序且可当选（排最后）
//	ExpiryUnusable 本次不可用（无启用账号/探不到）   剔除
//	ErrAborted     这次调用不该继续                  立刻返回 err，绝不折算成不可用
//
// ⚠️ 参考实现踩过的**真实缺陷**（PR !69 审计实测）：初版拿「最早作废时刻」的
// 初值 Infinity 当「还没选中任何渠道」的哨兵，于是**全部条目都是 NEVER 时循环
// 一次都不赋值** ⇒ 返回 undefined ⇒ 用户看到「没有任何已登录且可用的渠道账号，
// 请先登录」—— 而账号全都正常，只是积分都是永久的。这是「账号都在」被误报成
// 「没登录」。故本包用「状态枚举 + found 独立变量」分开记（见 PickSoonest）。
//
// ⚠️ Go 版额外的一条：JS 里 NEVER 是 +Infinity，两个 NEVER 相减得 NaN，而 NaN
// 不是合法的比较器返回值（sort 会当 0，顺序随引擎实现漂移）。本包改用显式状态
// 分支（见 candidateLess），连「先判 != 再相减」都不需要。
package aggregate

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"zenui/internal/zen/modelprice"
)

// 候选条目的三种状态。与参考实现的 NEVER / UNUSABLE 一一对应，但用字符串枚举
// 而不是 ±Infinity：数值哨兵会被算术悄悄改写成别的值（Inf-Inf=NaN 是最出名的
// 一例），枚举则只能被显式比较。
const (
	// ExpiryKnown 表示本次拿到了真实到期时刻，Expiry 字段有效。
	ExpiryKnown = "known"
	// ExpiryNever 表示**可用**，但只有永久/长期积分：参与排序、排在有临期的之后，
	// 但**可以当选**。它不是「不可用」。
	ExpiryNever = "never"
	// ExpiryUnusable 表示本次不可用（渠道未登录 / 没有启用账号 / 探测失败）。
	// 它会被 Rank 剔除，因此**不该出现在给界面的候选表里**。
	ExpiryUnusable = "unusable"
)

// ChannelRef 是「一条候选属于哪个渠道」的最小事实：渠道 id 与显示名。结算层
// 用它把候选换回 store.Provider（渠道 → 供应商的映射由调用方注入，本包不认识
// store）。
type ChannelRef struct {
	ID   string
	Name string
}

// Model 是某渠道目录里的一条模型条目（适配层从 store.Provider / lane 目录喂进来）。
type Model struct {
	ID      string
	Name    string
	Pricing modelprice.Info
	Rate *float64 // caller evaluates scheduled prices using its own clock
}

// Candidate 是一条「某渠道的某条真实模型」条目。
//
// Price 是**展示层解析**出来的倍率（见 PriceFactor）：各渠道的权威来源不同
// （qoder 在目录 priceFactor、buddy 在远端 config 拼进名字），故这里只认目录名
// 这一个所有渠道统一展示的地方。
type Candidate struct {
	Pricing  modelprice.Info
	Channel  string
	RealID   string
	RealName string
	Price    float64
	// Expiry 仅在 State == ExpiryKnown 时有效；其它状态一律零值，避免「一个
	// 看起来像时刻的零值」被下游误当成 1970 年到期。
	Expiry time.Time
	// State 是 ExpiryKnown / ExpiryNever / ExpiryUnusable 之一。
	State string
	// Wired 记录「该渠道在编译期清单里」这一事实（渠道资格），与本次是否真的
	// 可选（账号在不在）是两件事，界面上分开显示。
	Wired bool
	// Excluded 记录用户逐条关掉的候选。
	Excluded bool
	// Via 记录这条候选是用哪条规则归到规范键的（id / name / patch），供面板核查。
	Via string
}

// Key 是这条候选的排除键（channel|realId），与 store.Config.AggregateExclude
// 的写法一致。用 realId 而不是规范键：同一个虚拟模型下同一个渠道可能有多条候选，
// 用户关掉的是其中**具体那一条**。
func (c Candidate) Key() string { return c.Channel + "|" + c.RealID }

// VirtualModel 是一个规范键下的全部候选。
type VirtualModel struct {
	Key        string
	Name       string
	Candidates []Candidate
}

// --- 归一化 -------------------------------------------------------------------

// excludedAliases 是**抽象路由别名**，不是真实模型，一律不参与归一化。
//
// 两个来源：workbuddy 的 default-model / fast-model / balanced-model /
// primary-model / deep-model 是服务端抽象路由（由后端决定实际用哪个模型），
// qoder 系的 auto 是客户端选模模式。它们若参与归一化会变成若干「只有一个渠道」
// 的虚拟模型，语义上是空的 —— 用户选 aggregate/default-model 等于选了个我们
// 无法保证语义的东西。
var excludedAliases = map[string]bool{
	"default-model":  true,
	"fast-model":     true,
	"balanced-model": true,
	"primary-model":  true,
	"deep-model":     true,
	"auto":           true,
}

// nameChannel 是按**展示名**归一（而不是按 id）的渠道。
//
// ⚠️ 别「为了省事」把 qoder 改成 id 通道：它的 modelId 是 `gfmodel` 这类内部
// 代号，走 id 通道会让每个模型各自成一个孤立虚拟模型，跨渠道聚合**完全不发生**
// ⇒ 用户在聚合的轮换列表里永远看不到它，而症状是静默的（它只是不在候选表里）。
// 判据不是「哪个方便」，而是「该渠道的 id 对人类与其它渠道有没有信息」。
var nameChannel = map[string]bool{"qoder": true, "qodercn": true, "accio": true}

// canonicalPatches 是「渠道 → realId → 规范键」的显式补丁：两个通道都算不出正确
// 归属时才用。**每一条都必须有实测依据** —— 猜错会让请求打到错的模型，且没有
// 任何提示。未实测确认的不登记，宁可让它各自成一个单渠道虚拟模型。
var canonicalPatches = map[string]map[string]string{
	// 远端专有 id，其兜底表里没有它；实测展示名为 DeepSeek-V4.1-Flash。
	"lobsterai": {"deepseek-flash": "deepseek-v4-1-flash"},
	// accio 走 name 通道，展示名 "Qwen 3.8 Flash" 里的空格被归一成连字符 ⇒
	// 得 qwen-3-8-flash，而 loomy / trae / qoder 的 id 是 "qwen3.8-flash" ⇒
	// 得 qwen3-8-flash。同一模型两个键 ⇒ 拆成两个虚拟模型 ⇒ 轮换列表里看不到
	// 它，零报错零日志。这条补丁就是为此存在的。
	"accio": {"qwen-3-8-flash": "qwen3-8-flash"},
}

// canonicalNames 是规范键 → 官方展示名（用户在模型选择器里看到的名字）。
//
// ⚠️ 只登记我们**确定**的模型；未登记的回退为键本身（不编造）。编造一个「看起来
// 像官方」的名字比显示裸键更糟：用户无从判断它是不是自己想的那个模型。
// ⚠️ 展示名**不含倍率**：倍率是按渠道不同的运行时信息，写进规范名会让同一个虚拟
// 模型在不同渠道下显示不同名字，与「归一」的目的相反。
var canonicalNames = map[string]string{
	"deepseek-v4-1-flash":                 "DeepSeek V4.1 Flash",
	"deepseek-v4-flash":                   "DeepSeek V4 Flash",
	"deepseek-v4-flash-0731":              "DeepSeek V4 Flash 0731",
	"deepseek-v4-pro":                     "DeepSeek V4 Pro",
	"deepseek-flash":                      "DeepSeek Flash",
	"glm-5-3":                             "GLM-5.3",
	"glm-5-3-flash":                       "GLM-5.3-Flash",
	"glm-5-2":                             "GLM-5.2",
	"glm-5-1":                             "GLM-5.1",
	"glm-5":                               "GLM-5",
	"glm-5v-turbo":                        "GLM-5V-Turbo",
	"kimi-k3":                             "Kimi-K3",
	"kimi-k3-1":                           "Kimi-K3-1",
	"kimi-k2-8-preview":                   "Kimi-K2.8-Preview",
	"kimi-k2-7":                           "Kimi-K2.7",
	"kimi-k2-7-code":                      "Kimi-K2.7-Code",
	"kimi-k2-7-code-highspeed":            "Kimi-K2.7-Code-Highspeed",
	"kimi-k2-6":                           "Kimi-K2.6",
	"kimi-k2-5":                           "Kimi-K2.5",
	"minimax-m3":                          "MiniMax-M3",
	"minimax-m2-7":                        "MiniMax-M2.7",
	"minimax-m2-7-highspeed":              "MiniMax-M2.7-highspeed",
	"minimax-m3-1-flash-preview":          "MiniMax-M3.1-Flash-Preview",
	"qwen3-8-max":                         "Qwen3.8-Max",
	"qwen3-8-flash":                       "Qwen3.8-Flash",
	"qwen3-7-max":                         "Qwen3.7-Max",
	"qwen3-7-plus":                        "Qwen3.7-Plus",
	"qwen3-7-flash":                       "Qwen3.7-Flash",
	"qwen3-6-plus":                        "Qwen3.6-Plus",
	"qwen3-5-plus-2026-04-20":             "Qwen3.5-Plus-2026-04-20",
	"gemini-3-8-flash":                    "Gemini 3.8 Flash",
	"gemini-3-5-flash":                    "Gemini 3.5 Flash",
	"hy4-preview":                         "Hy4 Preview",
	"hy3":                                 "Hy3",
	"hy3-x":                               "Hy3 X",
	"sensenova-6-8-flash":                 "SenseNova-6.8-Flash",
	"sensenova-6-8-flash-lite":            "SenseNova-6.8-Flash-Lite",
	"doubao-seed-2-1-pro-260628":          "Doubao Seed 2.1 Pro",
	"doubao-seed-2-1-turbo-260628":        "Doubao Seed 2.1 Turbo",
	"doubao-seed-2-0-code-preview-260215": "Doubao Seed 2.0 Code Preview",
	"deepseek-v4-flash-official":          "DeepSeek V4 Flash Official",
}

// CanonicalDisplayName 是规范键 → 展示名；未登记的回退为键本身。
//
// 用带 ok 的 map 查找而不是取值判空：Go 里没有 JS 那条「constructor 会沿原型链
// 返回 Object 构造函数」的坑，但「未登记」与「登记成空串」仍要分开。
func CanonicalDisplayName(key string) string {
	key = NormalizeModelKey(key)
	if n, ok := canonicalNames[key]; ok && n != "" {
		return n
	}
	return key
}

// modelSpellingRules only reconcile known family spelling conventions.
var modelSpellingRules = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`^qwen-([0-9])`), `qwen${1}`},
	{regexp.MustCompile(`^(gpt|glm|gemini)([0-9])`), `${1}-${2}`},
	{regexp.MustCompile(`^(deepseek|kimi|minimax|mimo)-(v|k|m)-([0-9])`), `${1}-${2}${3}`},
	// Claude 的版本在档位之前/之后是两种常见的同名写法；后缀原样保留。
	{regexp.MustCompile(`^claude-([0-9]+(?:-[0-9]+)*)-(opus|sonnet|haiku)(.*)$`), `claude-${2}-${1}${3}`},
}

// NormalizeModelKey unifies namespaces, free-access markers and spelling while
// preserving version, tier, region, snapshot and contributor variants.
func NormalizeModelKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ":free")
	s = strings.TrimPrefix(s, "sn-")
	// 只统一拼写和分隔符，保留版本、档位、地区、日期和 contributor 等变体。
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '–' || r == '—' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}), "-")
	// free 是接入计费标记，与 OpenRouter 的 :free 同义，不是模型版本。
	s = strings.TrimSuffix(s, "-free")
	for _, rule := range modelSpellingRules {
		s = rule.re.ReplaceAllString(s, rule.to)
	}
	return s
}

// normalizeNameKey 把一条**展示名**归一成候选键：先剥掉非模型信息再走
// NormalizeModelKey。
//
// 要剥的东西：倍率后缀 " · x0.75" / " · x0.2→x0.1"、「免费」标注。只剥**第一个
// "·" 之后**的全部内容 —— "·" 是倍率分隔符（U+00B7），在参考实现里所有渠道的
// 展示名中都不作为模型名的一部分出现。
//
// ⚠️ 那条「免费」替换是**独立于 "·" 规则**的一道闸门：它只对不带 "·" 的裸
// 「免费」（"SenseNova-6.8-Flash 免费"）生效；带 "·" 的形态已被上一行连分隔符
// 一并剥掉，走不到这里。两种输入在单测里各有一条断言守着。
func normalizeNameKey(name string) string {
	s := name
	if i := strings.Index(s, "·"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "免费", "")
	return NormalizeModelKey(strings.TrimSpace(s))
}

// CanonicalKey 把一条真实模型归到规范键。
//
// 返回的 via 记「用什么规则归的」（id / name / patch），供面板核查 —— patch 说明
// 这条归一是人工实测结论，比任何推导都强。ok=false 表示这条条目**不归属任何
// 虚拟模型**（抽象路由别名，或归一后为空键）。
//
// 顺序（不可调换）：排除集 → 显式补丁 → 主通道 → 主通道没给出信息时回落另一通道。
//
// ⚠️ 排除判定必须在**算出键之后**再做一次，而不是只看 realId。参考实现的真实
// 缺陷：初版只查 realId / idKey，于是某条目的展示名归一后落在排除集里（展示名
// 就叫 "Auto"）而它的 id 不是 "auto"，就会被放行 ⇒ 产出一个 key == "auto" 的
// 虚拟模型，与聚合组的 auto 撞名：界面上两个 auto，且目录校验会因 id 重复把
// 整个聚合分组打成不可用。qoder 的 auto 恰好 realId 也是 "auto"，初版能挡住它
// 靠的是巧合 —— 换个 id 就漏。
func CanonicalKey(channel, realID, realName string) (key, via string, ok bool) {
	idKey := NormalizeModelKey(realID)
	if excludedAliases[strings.TrimSpace(realID)] || excludedAliases[idKey] {
		return "", "", false
	}
	if patch, has := canonicalPatches[channel]; has {
		if v, hit := patch[realID]; hit {
			if excludedAliases[v] {
				return "", "", false
			}
			return NormalizeModelKey(v), "patch", true
		}
	}
	if realName == "" {
		realName = realID
	}
	nameKey := normalizeNameKey(realName)
	chosen, chosenVia := idKey, "id"
	if nameChannel[channel] && nameKey != "" && nameKey != idKey {
		chosen, chosenVia = nameKey, "name"
	}
	if chosen == "" || excludedAliases[chosen] {
		return "", "", false
	}
	return chosen, chosenVia, true
}

// --- 虚拟模型表 ---------------------------------------------------------------

// BuildVirtualModels 从「渠道 → 该渠道目录」推导虚拟模型表。
//
// 输出约定两条，都是给界面用的稳定性契约：
//
//  1. **候选保留真实 id 与真实展示名**：面板子列表要逐行显示「哪个渠道的哪个
//     modelId」，用户据此核查合并是否正确并逐条拒绝；
//  2. **虚拟模型按候选数降序，同数按 key 升序；候选按渠道名升序、再按 realId
//     升序**：降序让冗余度高的模型在前（渠道多 = 更不容易挂），升序让面板与日志
//     的顺序可比对。
//
// ⚠️ 空 id（含纯空白）必须跳过，且判空要 trim 之后判。参考实现的评审实测：
// { id: ' ' } 归一后得到 key == ""、name == "" 的退化虚拟模型，而目录校验要求
// name 非空 ⇒ **整个聚合目录因此不可用**。真实目录给空白的概率极低，但代价是
// 全局的、修法是一行，故按「宁可多一道闸」处理。
//
// ⚠️ 同一虚拟模型下**同一个渠道**理论上可能有多条候选，这里都保留，让用户在子
// 列表里逐条看到并自己决定关哪一条。要触发它需要该渠道走 name 通道（qoder /
// qodercn / accio）且两条不同 realId 的展示名归一后相同。
func BuildVirtualModels(catalogs map[string][]Model) []VirtualModel {
	byKey := map[string][]Candidate{}
	seen := map[string]bool{}
	for channel, models := range catalogs {
		for _, m := range models {
			if strings.TrimSpace(m.ID) == "" {
				continue
			}
			realName := m.Name
			if realName == "" {
				realName = m.ID
			}
			key, via, ok := CanonicalKey(channel, m.ID, realName)
			if !ok {
				continue
			}
			identity := (Candidate{Channel: channel, RealID: m.ID}).Key()
			if seen[identity] {
				continue
			}
			seen[identity] = true
			price := PriceFactor(realName)
			if rate := m.Rate; rate != nil {
				price = *rate
			}
			byKey[key] = append(byKey[key], Candidate{
				Channel: channel, RealID: m.ID, RealName: realName,
				Price: price, Pricing: m.Pricing, Via: via,
			})
		}
	}
	out := make([]VirtualModel, 0, len(byKey))
	for key, cands := range byKey {
		sort.SliceStable(cands, func(i, j int) bool {
			if cands[i].Channel != cands[j].Channel {
				return cands[i].Channel < cands[j].Channel
			}
			return cands[i].RealID < cands[j].RealID
		})
		out = append(out, VirtualModel{Key: key, Name: CanonicalDisplayName(key), Candidates: cands})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Candidates) != len(out[j].Candidates) {
			return len(out[i].Candidates) > len(out[j].Candidates)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Inf 便于调用方与 PriceFactor 的「解析不出」比较时读得顺一点。
func Inf() float64 { return math.Inf(1) }
