package com.lampguide.lampnative.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.jsonObject
import mobile.Mobile

/**
 * AdminClient 是界面拿到数据的**唯一**通道。
 *
 * 它不做 HTTP：直接调 `mobile.Mobile.admin(path, bodyJSON, saveTo, loadFrom)`，
 * 内部走 zenbridge → 网关真实 router，和 HTTP API 使用者是同一份代码。
 * `/admin/…` 是 loopback-only 且校验 Host + RemoteAddr + Origin，自己发 HTTP
 * 请求只会被 403 拒掉。
 *
 * 三条硬规则：
 *
 * 1. **先解析再判 ok**。返回值是 JSON 字符串，用 startsWith 判会漏掉
 *    `{"code":"...","ok":false,...}` 这种键序不同的回答。
 * 2. **错误原文上屏**。信封里的 `error` 是网关自己的话（`签到正在执行，请等待
 *    本轮完成`、`地址已被占用`），界面照抄，不翻译成「操作失败」。
 * 3. 只喂白名单里的路径（见 [Admin]），别的一律当作不存在。
 */
class AdminClient(@PublishedApi internal val json: Json = Wire.json) {

    /** 一次 admin 调用的结果，只有两种：网关答了，或者网关没答/答错了。 */
    sealed interface Result<out T> {
        data class Ok<T>(val value: T) : Result<T>
        data class Err(val code: String, val message: String) : Result<Nothing>
    }

    // ---------------------------------------------------------------- 白名单

    object Admin {
        const val STATE = "state"
        const val SETTINGS = "settings"
        const val CHANNEL_STATE = "channel/state"
        const val CHANNEL_LOGIN = "channel/login"
        const val CHANNEL_TOKEN = "channel/token"
        const val CHANNEL_LOGOUT = "channel/logout"
        const val CHECKIN_STATE = "checkin/state"
        const val CHECKIN_RUN = "checkin/run"
        const val CHECKIN_SETTINGS = "checkin/settings"
        const val KEY_ROTATE = "key/rotate"
        const val REPROBE = "reprobe"
        const val AGGREGATE_STATE = "aggregate/state"
        const val MODELS_VISIBILITY = "models/visibility"
        const val LOGS = "logs"

        fun agent(id: String, enable: Boolean): String =
            "agent/$id/${if (enable) "enable" else "disable"}"

        /**
         * 单模型探测。[channel] 解决跨源重名：`deepseek-v4.1-flash` 在免费车道
         * 和 WorkBuddy 里各有一条，只发 id 会探到免费车道那条。
         */
        fun probe(modelId: String, channel: String = ""): String =
            if (channel.isBlank()) "probe/$modelId"
            else "probe/$modelId?channel=" + java.net.URLEncoder.encode(channel, "UTF-8")

        fun poll(channelId: String): String = "channel/poll?id=$channelId"
    }

    // ---------------------------------------------------------------- 入口

    /**
     * 原始信封。GET 时 body 传 null（网关收 "" 也是 GET），导出/导入类的文件
     * 参数这里一律不传：安卓走 SAF，字节由 Kotlin 侧搬运。
     */
    suspend fun raw(path: String, body: JsonElement? = null): Result<JsonElement> =
        withContext(Dispatchers.IO) {
            val rawJson = callMobile(path, body)
            android.util.Log.d("LampAdmin", "path=$path len=${rawJson.length} tail=${rawJson.takeLast(80)}")
            val envelope = try {
                json.parseToJsonElement(rawJson).jsonObject
            } catch (e: Exception) {
                return@withContext Result.Err(
                    EnvelopeCodes.PARSE_FAILED,
                    "网关回答不是合法 JSON：${e.message ?: rawJson.take(160)}",
                )
            }
            decode(envelope)
        }

    /**
     * 直接把信封解成具体类型。写入类回答（`{"ok":true}`）也走这条路：调用方按
     * [Result.Ok] 当「命令成功」处理，需要读回复内容时才用 [raw]。
     */
    suspend inline fun <reified T> get(path: String, body: JsonElement? = null): Result<T> {
        return when (val r = raw(path, body)) {
            is Result.Err -> r
            is Result.Ok -> decodeData(r.value)
        }
    }

    inline fun <reified T> decodeData(element: JsonElement): Result<T> = try {
        Result.Ok(json.decodeFromJsonElement<T>(element))
    } catch (e: Exception) {
        Result.Err(
            EnvelopeCodes.DECODE_FAILED,
            "网关回答与界面预期不符：${e.message ?: element.toString()}",
        )
    }

    /** 只关心「命令成不成」的调用（开关、签到、换 Key）。 */
    suspend fun command(path: String, body: JsonElement? = null): Result<JsonElement> = raw(path, body)

    // ---------------------------------------------------------------- 内部

    private fun callMobile(path: String, body: JsonElement?): String = try {
        // bodyJSON 为空字符串时 Go 侧按 GET 处理；null 元素同理。
        Mobile.admin(path, body?.toString() ?: "", "", "")
    } catch (e: Throwable) {
        // 库没加载（极少见）或 Go 侧 panic 都会落到这里：如实报，不静默返回空。
        """{"ok":false,"code":"${EnvelopeCodes.IO_ERROR}","error":"调用网关失败：${e.message ?: e.toString()}"}"""
    }

    private fun decode(m: JsonObject): Result<JsonElement> {
        val ok = (m["ok"] as? JsonPrimitive)?.content?.toBooleanStrictOrNull() ?: false
        if (ok) {
            return Result.Ok(m["data"] ?: JsonPrimitive(""))
        }
        val code = (m["code"] as? JsonPrimitive)?.content?.takeIf { it.isNotBlank() }
            ?: EnvelopeCodes.UNKNOWN
        val error = (m["error"] as? JsonPrimitive)?.content?.takeIf { it.isNotBlank() }
            ?: "网关没有说明原因"
        return Result.Err(code, error)
    }
}

/** 信封 code 的稳定字符串，与 `internal/host/host.go` 的常量逐字对齐。 */
object EnvelopeCodes {
    const val GATEWAY_DOWN = "gateway_down"
    const val BAD_PAYLOAD = "bad_payload"
    const val IO_ERROR = "io_error"
    const val GATEWAY_REJECTED = "gateway_rejected"
    const val PARSE_FAILED = "parse_failed"

    /** host 侧导入失败的真实常量是 settings_parse_failed（见 NOTES）。 */
    const val SETTINGS_PARSE_FAILED = "settings_parse_failed"

    /** 客户端自己产生的两种：不算网关 fault，但界面同样要说话。 */
    const val DECODE_FAILED = "decode_failed"
    const val UNKNOWN = "unknown_error"
}

internal object Wire {
    /** ignoreUnknownKeys：网关多给一个字段不该让整页变成错态。 */
    val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        explicitNulls = false
        encodeDefaults = true
    }
}
