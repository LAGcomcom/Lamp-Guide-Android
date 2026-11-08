package aggregate

// 选型口径：倍率解析、候选排序、临期优先挑号。本文件只放**决策**，不碰 IO。

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrAborted 是「这次调用不该继续」的哨兵错误，调用方用 errors.Is 判它，而不是
// 看文案。参考实现的硬要求：中止**不得**被吞成「不可用」—— 用户取消请求后看到
// 「请先登录」是误导（真实缺陷，PR !69 审计实测）。
var ErrAborted = errors.New("aggregate: 选型已中止")

// ErrNoCandidates：一条候选都没有（真的没有任何可用渠道）。
var ErrNoCandidates = errors.New("aggregate: 没有任何可用候选")

// ErrAllPermanent：候选都在，但积分都是永久/长期那一类，没有临期额度可烧。
//
// ⚠️ 它与 ErrNoCandidates 是**两个不同结论**，故各占一个错误值。参考实现把前者
// 误报成后者（「没有任何已登录且可用的 provider 账号，请先登录」），而账号全都
// 正常 —— 见 aggregate.go 模块头那条真实缺陷。
var ErrAllPermanent = errors.New("aggregate: 候选都在，但都没有临期额度（全是永久/长期积分）")

// --- 倍率解析 -----------------------------------------------------------------

// priceRe 是**倍率前必须有分隔符**这条硬规则的正则形态：分隔符是间隔号「·」或
// 乘号「×」，它在**必选**位置；分隔符后可选一个 x，再后面是数字；促销箭头（→ / >）
// 右侧的数值优先。
//
// ⚠️ 这就是参考实现的修法本身 —— 别把它放宽成「x + 数字 + 行尾」而不要求分隔符，
// 那会把 Spark X2.5 认成 2.5 倍（见 PriceFactor 的误判表）。注意「×4」里的 ×
// 本身就是分隔符，它后面不需要再补一个 x，故 `·` 与 `×` 是二选一而不是串联。
var priceRe = regexp.MustCompile(`(?:·|×)\s*[×x]?\s*(\d+(?:\.\d+)?)(?:\s*[→>]\s*[×x]?\s*(\d+(?:\.\d+)?))?\s*$`)

// PriceFactor 从展示名解析倍率：带「免费」返回 0；解析不出返回 math.Inf(1)
// （排在任何有标注的之后）。
//
// ## 为什么必须要求倍率前有分隔符（参考实现的真实缺陷，PR !69 审计实测）
//
// 初版正则只要求「x + 数字 + 行尾」，不要求分隔符 ⇒ 模型名里的 x 被当成倍率：
//
//	展示名                          初版解析   应为
//	Spark X2.5（无倍率标注）        2.5 ❌     Inf
//	GPT-X5                          5   ❌     Inf
//	Grok x4                         4   ❌     Inf
//	DeepSeek V4 Flash 0731 · x3.0   3.0 ✅     3.0
//
// Spark X2.5 这条路径**真实可达**：远端下发不带倍率的名称时被原样写进目录名，
// 于是「挑最便宜」反而挑到了最贵的 —— 同池里 `· x0.8` 的模型排到了它后面。
// 这类缺陷是**静默**的：不报错、不告警，只是钱多花了一点。
//
// 认得的形态只有三种（都是参考实现实测到的真实形态）：
//
//	… · x3.0         间隔号（U+00B7）+ 可选 x + 数字
//	… ×4             乘号
//	… · x0.17→x0.50  促销：取**箭头右侧**，那才是实际计费倍率
//
// ⚠️ 只做展示层解析：各渠道的倍率权威来源不同（qoder 在目录 priceFactor、
// buddy 在远端 config 拼进名字），逐家接字段是个大工程，而目录名是所有渠道统一
// 展示的地方 —— 先用这一层。
func PriceFactor(name string) float64 {
	if strings.Contains(name, "免费") {
		// 免费档没有倍率概念：按 0 记，排序时落在最便宜那一档。
		return 0
	}
	m := priceRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return math.Inf(1)
	}
	raw := m[1]
	if m[2] != "" {
		raw = m[2] // 促销箭头：取右侧（实际计费倍率），不是被划掉的那个
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || v < 0 {
		return math.Inf(1)
	}
	return v
}

// --- 排序 ---------------------------------------------------------------------

// ManualRank 报告一条候选的手工位次；ok=false 表示用户没排过它。
type ManualRank func(Candidate) (int, bool)

// Rank 排出候选链：**手工位次严格优先 → 临期升序 → 倍率升序 → 声明顺序兜底**。
//
// 四个判据：
//
//   - 第一判据「手工位次」（仅当传入非 nil 的 manualRank）：用户拖出来的顺序，
//     排过的一律排在**全部**没排过的之前；
//   - 第二判据「临期升序」：越早作废的先用掉（本功能的全部价值）；
//   - 第三判据「倍率升序」：同一临期档里挑便宜的（省钱）；
//   - 第四判据「声明顺序」：完全并列时保持候选表顺序（sort.SliceStable 保证）。
//
// ⚠️⚠️ 手工位次是「严格序 + 临期兜底」，**不是**「临期为主、手工为辅」。用户
// 拖动时表达的是「我要这个渠道先被用」；若临期仍优先，拖动会被临期值**静默覆盖**
// ⇒ 面板显示的顺序与真实轮换顺序分叉。
//
// ⚠️ **不可用（State == ExpiryUnusable）的候选一律剔除**，且剔除发生在排序之前。
// ⚠️ **纯函数**：不改传入切片（先复制）。调用方会对同一份候选表反复排序（每次
// 请求），原地排序会让顺序随调用次数漂移。
//
// ⚠️ Go 与 JS 的一处关键差异：参考实现用 Infinity 当「无临期」哨兵，靠「先判
// != 再相减」绕开 Infinity-Infinity=NaN（NaN 不是合法的比较器返回值，sort 会当
// 0，顺序随引擎实现漂移）。本包用显式状态枚举 + 显式分支（见 candidateLess），
// **不要**把这里改回「相减返回 float」的写法 —— 两个 Never 候选相减就又是 NaN。
func Rank(candidates []Candidate, manualRank ManualRank) ([]Candidate, error) {
	out := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.State == ExpiryUnusable {
			continue
		}
		out = append(out, c)
	}
	rankOf := func(c Candidate) int {
		if manualRank == nil {
			return math.MaxInt
		}
		if r, ok := manualRank(c); ok {
			return r
		}
		return math.MaxInt
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ra, rb := rankOf(a), rankOf(b)
		if ra != rb {
			return ra < rb
		}
		return candidateLess(a, b)
	})
	return out, nil
}

// candidateLess 是 Rank 的第二、三判据，单独抽出来是为了让哨兵语义**只写一遍**。
//
// 显式分支：Known 与 Known 比时刻；Known 永远排在 Never 之前；两者都是 Never
// 时直接落到倍率，绝不拿哨兵做减法。
func candidateLess(a, b Candidate) bool {
	ak, bk := a.State == ExpiryKnown, b.State == ExpiryKnown
	switch {
	case ak && bk:
		if !a.Expiry.Equal(b.Expiry) {
			return a.Expiry.Before(b.Expiry)
		}
	case ak != bk:
		// 有真实到期时刻的排在没有到期时刻的前面（临期先烧）。
		return ak
	}
	return priceLess(a.Price, b.Price)
}

// priceLess 比较两个倍率，Inf 参与比较但**不做减法**：NaN 不进比较器。
//
// 「解析不出的倍率」（NaN）与「解析不出」在参考实现里是 Infinity，本包保留
// Inf 语义但不接受 NaN：真收到 NaN 就按「最贵」处理（排最后），绝不让它污染
// 比较器 —— sort 拿到不一致的比较结果会给出随机的顺序，而那正是「界面顺序与
// 真实轮换顺序分叉」的另一种成因。
func priceLess(a, b float64) bool {
	an, bn := math.IsNaN(a), math.IsNaN(b)
	switch {
	case an && bn:
		return false
	case an:
		return false
	case bn:
		return true
	}
	return a < b
}

// PickSoonest 从候选表里挑出胜者：剔除不可用，其余按临期升序，并列时保留声明
// 顺序（即先出现的那个）。
//
// 返回的错误把三种「挑不出来」的情形分开说：
//
//	没有条目 / 全部不可用  → ErrNoCandidates（真的没登录或没账号）
//	条目都在但全是永久     → ErrAllPermanent（账号都在，只是没有临期可烧）
//
// ⚠️ 参考实现的初版把这两种塞进一个变量（拿「最早作废时刻」的初值当「还没选中」
// 的哨兵），于是全部条目都是永久时循环一次都不赋值 ⇒ 返回空 ⇒ 用户看到
// 「请先登录」，而账号全都正常。这正是本包把哨兵拆成「状态枚举 + found 变量」的
// 原因：found 与 best 是两个独立事实，绝不互相兼任。
//
// entries 的顺序即声明顺序，调用方传候选表顺序即可。
func PickSoonest(entries []Candidate) (Candidate, error) {
	var best Candidate
	found := false
	sawAny := false
	for _, c := range entries {
		if c.State == ExpiryUnusable {
			continue
		}
		sawAny = true
		// 严格小于：并列时保留先出现的，不重排。
		if !found || candidateLess(c, best) {
			best = c
			found = true
		}
	}
	switch {
	case found:
		return best, nil
	case sawAny:
		return Candidate{}, ErrAllPermanent
	default:
		return Candidate{}, ErrNoCandidates
	}
}

// --- 时间口径 -----------------------------------------------------------------

// NextDayStart 返回 t 所在时区**次日零点**。用于「今日赠送额度当日 24:00 作废」
// 这一类没有逐笔时间戳的渠道（loomy / zcode 的每日额度）。
//
// ⚠️ 别用「猜一个 UTC+8 零点」去消除其它渠道的未知重置时刻 —— 那是凭空造一个
// 上游从未承诺的时间（参考实现的红线「绝不编造数字」）。这个函数只服务那些
// **明确按自然日重置**的额度。
func NextDayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}
