package com.lampguide.lampnative.ui.theme

import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** 数字与时间的显示格式。**只做格式化，不做业务推算**：网关给什么数就显示什么。 */
object Fmt {

    /** 大数字：1234 → 1.2k，1.23M → 1.2M。 */
    fun tokens(n: Long): String = when {
        n < 0 -> "0"
        n < 1_000 -> n.toString()
        n < 1_000_000 -> trim(n / 1_000.0) + "k"
        else -> trim(n / 1_000_000.0) + "M"
    }

    fun tokens(n: Int): String = tokens(n.toLong())

    private fun trim(v: Double): String {
        val s = String.format(Locale.US, "%.1f", v)
        return s.removeSuffix(".0")
    }

    fun ms(v: Long): String = when {
        v <= 0 -> "—"
        v < 1_000 -> "${v}ms"
        v < 60_000 -> trim(v / 1000.0) + "s"
        else -> "${v / 60_000}m"
    }

    /** 上下文窗口：128000 → 128k。 */
    fun context(v: Int): String = when {
        v <= 0 -> "—"
        v >= 1_000_000 -> trim(v / 1_000_000.0) + "M"
        v >= 1_000 -> "${v / 1_000}k"
        else -> v.toString()
    }

    fun uptime(sec: Long): String {
        if (sec <= 0) return "—"
        val d = sec / 86400
        val h = (sec % 86400) / 3600
        val m = (sec % 3600) / 60
        return when {
            d > 0 -> "${d}天${h}小时"
            h > 0 -> "${h}小时${m}分"
            else -> "${m}分${sec % 60}秒"
        }
    }

    /** epoch ms → MM-dd HH:mm；0 显示为「—」。 */
    fun stamp(ms: Long): String {
        if (ms <= 0) return "—"
        return SimpleDateFormat("MM-dd HH:mm", Locale.CHINA).format(Date(ms))
    }

    fun clock(ms: Long): String {
        if (ms <= 0) return "—"
        return SimpleDateFormat("HH:mm", Locale.CHINA).format(Date(ms))
    }

    /** 「还剩 3 天」这类相对天数；读的是网关给的时间戳，不是本地猜的。 */
    fun remaining(expiryMs: Long): String {
        if (expiryMs <= 0) return "—"
        val diff = expiryMs - System.currentTimeMillis()
        if (diff <= 0) return "已过期"
        val days = diff / 86_400_000L
        return if (days >= 1) "还剩 ${days} 天" else "不到 1 天"
    }
}
