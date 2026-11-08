package com.lampguide.lampnative.data

import com.lampguide.lampnative.ui.theme.Fmt
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive

/**
 * 信封 code → 界面表现的**唯一**对照表。
 *
 * 每种失败都有不同的脸：网关没起是整页红块 +「启动」；业务拒绝是这条命令的
 * 原话（不吞）；导入文件不是 JSON 要说「文件不是合法配置」。一律弹「操作失败」
 * 是最容易假装成功的地方。
 */
enum class EnvelopeFace(val title: String, val showStart: Boolean) {
    Down("网关没有启动", true),
    Rejected("网关拒绝了这条命令", false),
    BadPayload("界面参数有误", false),
    Io("文件读写失败", false),
    Parse("文件不是合法配置", false),
    Decode("网关回答与界面预期不符", false),
    Unknown("网关没有说明原因", false),
    ;

    companion object {
        fun of(code: String): EnvelopeFace = when (code) {
            EnvelopeCodes.GATEWAY_DOWN -> Down
            EnvelopeCodes.GATEWAY_REJECTED -> Rejected
            EnvelopeCodes.BAD_PAYLOAD -> BadPayload
            EnvelopeCodes.IO_ERROR -> Io
            EnvelopeCodes.SETTINGS_PARSE_FAILED, EnvelopeCodes.PARSE_FAILED -> Parse
            EnvelopeCodes.DECODE_FAILED -> Decode
            else -> Unknown
        }
    }
}

/** 一个界面上的错误：code 决定脸，message 是网关原话。 */
data class UiError(val code: String, val message: String) {
    val face: EnvelopeFace get() = EnvelopeFace.of(code)
}

/** 模型可用性的四种脸，与 `lane.FreshState` 的取值一一对应。 */
enum class ModelState(val label: String) {
    Available("可用"),
    Throttled("限流"),
    RegionBlocked("区域限制"),
    Unavailable("不可用"),
    Unknown("未探测"),

    /**
     * 自建渠道（用户自己加的 API）里的模型，网关还没替它测过。
     *
     * 原来写作「自定义」，用户读成「这是个自定义模型」，而不是「还没测」——
     * 真实后果是：这一行既不进「可用」计数、又默认被筛掉，于是 Qoder 里明明
     * 有的 DeepSeek 在模型页看不见。现在叫「待测」，并参与默认筛选。
     */
    Custom("待测"),
    ;

    companion object {
        fun of(raw: String): ModelState = when (raw.trim().lowercase()) {
            "available" -> Available
            "throttled" -> Throttled
            "region-blocked", "region_blocked", "regionblocked" -> RegionBlocked
            "unavailable" -> Unavailable
            "custom" -> Custom
            else -> Unknown
        }
    }
}

/**
 * 账号行的派生标签：只读网关给的 `result`，不重算业务。
 *
 * `result.updatedAt` 落在今天（`today`）且 status 是成功值就算「今天已签到」；
 * 副标题直接用 `result.message`，不自己编文案。
 */
data class CheckinBadge(val done: Boolean, val text: String, val ok: Boolean)

object Mappers {

    private const val SUCCESS_STATUS_SET_JSON =
        "checked_in,success,already,claimed,ok,completed"

    /** 签到结果的成功取值。网关侧 status 是开放字符串，这里只认已知的几个。 */
    private val successStatuses = setOf(
        "checked_in", "success", "already", "claimed", "ok", "completed",
    )

    /** 今日是否已签到；判断只用 view.today 与 result.day/updatedAt，不猜时区。 */
    fun checkin(row: CheckinRow, view: CheckinView): CheckinBadge {
        val status = row.result.status.lowercase()
        val day = row.result.day.ifBlank { "" }
        val sameDay = day == view.today || (
            row.result.updatedAt > 0 && day.isBlank()
            )
        val done = sameDay && status in successStatuses
        val text = when {
            row.result.message.isNotBlank() -> row.result.message
            status.isBlank() -> "还没有跑过"
            else -> status
        }
        return CheckinBadge(done = done, text = text, ok = done || status in successStatuses)
    }

    /** 今天这一天的统计；没有这一天就是全零（不是错误）。 */
    fun today(stats: Stats, todayKey: String): DayStat = stats.days[todayKey] ?: DayStat()

    /**
     * 用量页的 7 天柱 + Top5。
     *
     * 唯一的算术在这里：把近 7 天每天的 `output` 加起来、把每天的 `models` 按
     * 模型累加取前 5。**这两个都是同一份数据的再呈现**，不是额度/可用性推算
     * —— 后者一律只读网关给的值。
     */
    fun sevenDays(stats: Stats, todayIso: String): List<DayBar> {
        val fmt = java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd")
        val today = java.time.LocalDate.parse(todayIso, fmt)
        return (6 downTo 0).map { back ->
            val date = today.minusDays(back.toLong())
            val key = date.format(fmt)
            val d = stats.days[key]
            DayBar(
                date = key,
                label = "${date.monthValue}/${date.dayOfMonth}",
                output = d?.output ?: 0,
                requests = d?.requests ?: 0,
            )
        }
    }

    fun topModels(stats: Stats, todayIso: String, limit: Int = 5): List<ModelSpend> {
        val fmt = java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd")
        val today = java.time.LocalDate.parse(todayIso, fmt)
        val sums = mutableMapOf<String, Long>()
        val reqs = mutableMapOf<String, Int>()
        for (back in 0..6) {
            val key = today.minusDays(back.toLong()).format(fmt)
            val d = stats.days[key] ?: continue
            for ((id, tok) in d.models) sums[id] = (sums[id] ?: 0L) + tok.toLong()
            for ((id, n) in d.modelReqs) reqs[id] = (reqs[id] ?: 0) + n
        }
        return sums.entries
            .sortedByDescending { it.value }
            .take(limit)
            .map { ModelSpend(id = it.key, tokens = it.value, requests = reqs[it.key] ?: 0) }
    }

    /**
     * 模型行的副标题：只放**不进表格列**的定性信息（网关给的原话 detail）。
     *
     * 首字 / 上下文 / 今日 token 已经在各自的数字列里右对齐了，副标题再把它们
     * 拼一句，等于同一组数字在一行里出现两次，还把 detail 挤成省略号。
     * 没有 detail 就返回空串，行里不留第二行。
     */
    fun modelSubtitle(m: ModelRow): String = m.detail.trim()

    /** 首字延迟（优先中位数，没有就用单次值）。 */
    fun ttftOf(m: ModelRow): String = when {
        m.ttftMedMs > 0 -> Fmt.ms(m.ttftMedMs)
        m.ttftMs > 0 -> Fmt.ms(m.ttftMs)
        else -> "—"
    }

    /** 今日已用 token。 */
    fun quotaOf(m: ModelRow): String = if (m.quotaUsed > 0) Fmt.tokens(m.quotaUsed) else "—"

    /** `×N` 角标：候选数就是 candidates 的长度，不做归一化推算。 */
    fun candidatesOf(agg: AggregateState, modelId: String): Int? {
        val hit = agg.virtual.firstOrNull { it.key == modelId || it.name == modelId }
        return hit?.candidates?.size?.takeIf { it > 1 }
    }

    /** 主 Key 与专属 Key 的展示形：前 8 位 + …，绝不打全串。 */
    fun maskKey(key: String): String =
        if (key.length <= 8) key else key.take(8) + "…"

    /** agentKeys["dsh-go"] 的指纹：用于「Key 变了要说出来」的黄条比对。 */
    fun keyFingerprint(key: String): String =
        java.security.MessageDigest.getInstance("SHA-256")
            .digest(key.toByteArray())
            .joinToString("") { b -> b.toString(16).padStart(2, '0') }
            .take(16)

    fun jsonText(e: JsonElement?): String = when (e) {
        null -> "—"
        is JsonPrimitive -> e.content
        else -> e.toString()
    }

    /**
     * 「可用模型数」：available 的那些。
     *
     * 自建渠道的「待测」不并进来——那会把没测过的算成能用。但界面上必须让它
     * 出现在默认筛里（见 S4），否则用户会以为渠道里的模型不存在。
     */
    fun availableCount(models: List<ModelRow>): Int =
        models.count { ModelState.of(it.state) == ModelState.Available }
    /** 还没有探测过的模型数：这些既不算可用，也不算不可用。 */
    fun unprobedCount(models: List<ModelRow>): Int =
        models.count { ModelState.of(it.state) == ModelState.Unknown }

    /** 自建渠道里还没测过的条数（state=「待测」）。 */
    fun pendingCount(models: List<ModelRow>): Int =
        models.count { ModelState.of(it.state) == ModelState.Custom }

    /**
     * 「一个都不可用」必须是真的判过：还剩一堆 unknown 时那句话是错的 ——
     * 网关刚起来的那几分钟，整张表都是 unknown，界面却会说「一个模型都不可用」。
     */
    fun allProbed(models: List<ModelRow>): Boolean =
        models.isNotEmpty() && unprobedCount(models) == 0

    /** 探测中 / 刷新中这类「现在正在忙」的行；拿不到就整行隐藏，不画空进度条。 */
    fun busyLabel(state: StateView): String =
        if (state.probingModel.isNotBlank()) "正在探测 ${state.probingModel}" else ""
}

data class DayBar(val date: String, val label: String, val output: Int, val requests: Int)

data class ModelSpend(val id: String, val tokens: Long, val requests: Int)
