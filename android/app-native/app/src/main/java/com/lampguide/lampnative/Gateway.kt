package com.lampguide.lampnative

import android.content.Context
import mobile.Mobile

/**
 * Gateway 是 Go 网关的进程内句柄，把 `mobile.Mobile` 的四种真实失败折成可显示的
 * 结果。它是**唯一**碰 `Mobile.start` 的地方：`start` 是唯一抛异常的方法，
 * 而它的 message 就是第 6.2 节要判 `address already in use` 原文。
 */
object Gateway {

    const val DEFAULT_PORT = 8787

    /** 启动结果：成功带着真实 baseURL；失败带着 Go 侧原文，不翻译。 */
    sealed interface Boot {
        data class Up(val baseURL: String, val port: Int) : Boot
        data class Down(val reason: String) : Boot
    }

    fun homeDir(context: Context): String =
        java.io.File(context.filesDir, "zen-gate").absolutePath

    /**
     * 起网关。端口被占（8787 是 DSH Go 里存的那一个，不换端口）时先
     * `Mobile.stop()` 清一遍再重试一次；仍失败就把真实原因抬给调用方，
     * 由它写进通知和首屏。
     */
    fun start(context: Context, port: Int = DEFAULT_PORT): Boot {
        if (Mobile.isRunning()) {
            val url = runCatching { Mobile.baseURL() }.getOrNull().orEmpty()
            if (url.isNotBlank()) return Boot.Up(url, portOf(url, port))
        }
        firstTry(context, port)?.let { return it }

        // 一次重试：把上一轮没退干净的监听清掉。仍失败就如实报。
        runCatching { Mobile.stop() }
        firstTry(context, port)?.let { return it }
        return Boot.Down(
            lastFailure ?: "网关启动失败，且没有拿到 Go 侧的错误原文",
        )
    }

    private var lastFailure: String? = null

    private fun firstTry(context: Context, port: Int): Boot? {
        return try {
            Mobile.start(homeDir(context), port.toLong())
            lastFailure = null
            val url = runCatching { Mobile.baseURL() }.getOrNull().orEmpty()
            Boot.Up(url, portOf(url, port))
        } catch (e: Exception) {
            val msg = e.message ?: e.toString()
            lastFailure = msg
            if (msg.contains("address already in use", ignoreCase = true) ||
                msg.contains("Only one usage of each socket address", ignoreCase = true)
            ) {
                null // 交给上面重试一次
            } else {
                Boot.Down(msg)
            }
        }
    }

    fun stop() {
        runCatching { Mobile.stop() }
    }

    fun isRunning(): Boolean = runCatching { Mobile.isRunning() }.getOrDefault(false)

    fun baseURL(): String = runCatching { Mobile.baseURL() }.getOrDefault("")

    fun statusURL(): String = runCatching { Mobile.statusURL() }.getOrDefault("")

    /** `port()` 返回 Long，不是 Int——这里显式收窄，0 表示没在跑。 */
    fun port(): Int = runCatching { Mobile.port().toInt() }.getOrDefault(0)

    fun version(): String = runCatching { Mobile.version() }.getOrDefault("")

    private fun portOf(baseURL: String, fallback: Int): Int {
        val m = Regex(""":(\d+)/""").find(baseURL) ?: return fallback
        return m.groupValues[1].toIntOrNull() ?: fallback
    }
}

/**
 * PeerReport 回答一个问题：这台手机上装了 DSH Go 没有。
 *
 * 网关的 Agent 面要区分「这台机器上没有 DSH Go」与「没检测到配置文件」；
 * 后者它能自己查，前者只有宿主能查（另一个 APK 的 proot rootfs 在应用私有
 * 存储里）。报告是纯事实，不做任何注入。
 *
 * §6.1：manifest 里必须声明 <queries> 只针对那一个包名，否则
 * getPackageInfo 恒抛 NameNotFoundException，已装的 App 会被报成未安装。
 */
object PeerReport {

    const val DSH_GO = "io.github.ccch1mneyyy.dsh_app"

    fun report(context: Context) {
        val pm = context.packageManager
        val info = try {
            pm.getPackageInfo(DSH_GO, 0)
        } catch (e: Exception) {
            null
        }
        val payload = if (info == null) {
            """{"dshGo":false}"""
        } else {
            val name = info.versionName ?: ""
            val pkg = info.packageName ?: DSH_GO
            """{"dshGo":true,"dshGoVersion":"$name","dshGoPackage":"$pkg"}"""
        }
        runCatching { Mobile.setPeers(payload) }
    }
}
