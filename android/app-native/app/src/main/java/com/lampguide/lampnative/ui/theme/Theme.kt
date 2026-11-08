package com.lampguide.lampnative.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/**
 * 桌面版 Fluent 语言的原生移植。
 *
 * 数值出处与桌面工程的 `web/styles/tokens.css` 一一对应：地面 #F3F3F3 / #202020、
 * 卡片是半透明白（0.70）贴在地面上、强调色 #005FB8 / #60CDFF、圆角 4px。
 * 桌面版那套"半透明卡片 + 细描边 + 一档 hover"才是这个产品的长相，手机版沿用
 * 同一组语义名，所以两边可以并排认出来是同一个东西。
 *
 * 与桌面版的唯一偏离：手机没有侧栏，主色用量更省——琥珀只留给真正要动手的
 * 那个按钮，其余层级靠明度差读出来。
 */
@Immutable
data class LampPalette(
    val ground: Color,
    val surface: Color,
    val surfaceHigh: Color,
    val surfaceSolid: Color,
    val line: Color,
    val lineStrong: Color,
    val amber: Color,
    val amberSoft: Color,
    val onAmber: Color,
    /** 破坏性动作的实心底上的字色。 */
    val onBad: Color,
    val text: Color,
    val textDim: Color,
    val textFaint: Color,
    val ok: Color,
    val warn: Color,
    val bad: Color,
    val overlay: Color,
    val isDark: Boolean,
)

/** 深色：#202020 地面，卡片 0.061 白，强调 #60CDFF。 */
val DarkLamp = LampPalette(
    ground = Color(0xFF202020),
    surface = Color(0x0FFFFFFF),
    surfaceHigh = Color(0x14FFFFFF),
    surfaceSolid = Color(0xFF2C2C2C),
    line = Color(0x18FFFFFF),
    lineStrong = Color(0x29FFFFFF),
    amber = Color(0xFF60CDFF),
    amberSoft = Color(0x1F60CDFF),
    onAmber = Color(0xFF000000),
    onBad = Color(0xFFFFFFFF),
    text = Color(0xE4FFFFFF),
    textDim = Color(0x9AFFFFFF),
    textFaint = Color(0x72FFFFFF),
    ok = Color(0xFF6CCB5F),
    warn = Color(0xFFFCE100),
    bad = Color(0xFFFF99A4),
    overlay = Color(0x73000000),
    isDark = true,
)

/** 浅色：#F3F3F3 地面，卡片 0.70 白，强调 #005FB8。 */
val LightLamp = LampPalette(
    ground = Color(0xFFF3F3F3),
    surface = Color(0xB3FFFFFF),
    surfaceHigh = Color(0xE6FFFFFF),
    surfaceSolid = Color(0xFFF9F9F9),
    line = Color(0x0F000000),
    lineStrong = Color(0x29000000),
    amber = Color(0xFF005FB8),
    amberSoft = Color(0x1A005FB8),
    onAmber = Color(0xFFFFFFFF),
    onBad = Color(0xFFFFFFFF),
    text = Color(0xE4000000),
    textDim = Color(0x9A000000),
    textFaint = Color(0x72000000),
    ok = Color(0xFF0F7B0F),
    warn = Color(0xFF9D5D00),
    bad = Color(0xFFC42B1C),
    overlay = Color(0x4D000000),
    isDark = false,
)

val LocalLampPalette = staticCompositionLocalOf { DarkLamp }

object Lamp {
    val colors: LampPalette
        @Composable get() = LocalLampPalette.current
}

/** 桌面版圆角：卡 4、控件 4、浮层 8。手机屏小，控件统一走 8 才按得准。 */
object Radius {
    const val CARD = 8
    const val CONTROL = 8
    const val FLYOUT = 12
    const val PILL = 100
}

private fun scheme(p: LampPalette) = if (p.isDark) {
    darkColorScheme(
        primary = p.amber, onPrimary = p.onAmber, secondary = p.amber,
        background = p.ground, onBackground = p.text,
        surface = p.ground, onSurface = p.text, surfaceVariant = p.surfaceHigh,
        onSurfaceVariant = p.textDim, outline = p.line, error = p.bad,
    )
} else {
    lightColorScheme(
        primary = p.amber, onPrimary = p.onAmber, secondary = p.amber,
        background = p.ground, onBackground = p.text,
        surface = p.ground, onSurface = p.text, surfaceVariant = p.surfaceHigh,
        onSurfaceVariant = p.textDim, outline = p.line, error = p.bad,
    )
}

/**
 * 字号跟桌面版走：页标题 28、卡片标题 14、说明 13。
 * 手机上一屏能读的行更少，所以正文字号全部保留桌面档位，不再往下压——
 * 压到 12 会让「渠道说明」这类真句子在 6 寸屏上读不成句。
 */
private val LampTypography: Typography
    get() = Typography().let { base ->
        base.copy(
            displayLarge = TextStyle(fontSize = 40.sp, lineHeight = 46.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.5).sp),
            headlineSmall = TextStyle(fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold),
            titleMedium = TextStyle(fontSize = 15.sp, lineHeight = 21.sp, fontWeight = FontWeight.SemiBold),
            bodyMedium = TextStyle(fontSize = 14.sp, lineHeight = 20.sp),
            bodySmall = TextStyle(fontSize = 13.sp, lineHeight = 18.5.sp),
            labelSmall = TextStyle(fontSize = 12.sp, lineHeight = 16.sp),
        )
    }

@Composable
fun LampTheme(darkTheme: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val palette = if (darkTheme) DarkLamp else LightLamp
    CompositionLocalProvider(LocalLampPalette provides palette) {
        MaterialTheme(colorScheme = scheme(palette), typography = LampTypography, content = content)
    }
}
