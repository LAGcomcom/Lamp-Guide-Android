package com.lampguide.lampnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.UiError
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.Radius

/**
 * 全应用共用的几个小件，形状照着桌面版 SettingsCard 来：
 *
 *   图标 20 · 标题 14 半粗 · 说明 13 次级 · 右侧控件 —— 一张 4px 圆角的半透明卡。
 *
 * 它们只负责「长什么样」，四态判断在各屏自己写——因为每一屏的空态/错态说的是
 * 不同的话（模型页说「上游都不通」，用量页说「今天还没有请求」）。
 */

@Composable
fun ScreenScaffold(
    title: String,
    subtitle: String? = null,
    trailing: @Composable (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(start = 20.dp, end = 16.dp, top = 14.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    title,
                    color = Lamp.colors.text,
                    style = MaterialTheme.typography.headlineSmall,
                )
                if (subtitle != null) {
                    Text(
                        subtitle,
                        color = Lamp.colors.textDim,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
            if (trailing != null) trailing()
        }
        Box(Modifier.fillMaxWidth().weight(1f)) { content() }
    }
}

/**
 * 一张卡片。桌面版那张卡有三个要点，缺一个就不像它：
 * 半透明的面（贴在地面上，不是浮在上面）、1px 细描边、4px 圆角。
 * `emphasized` 只抬一档填充，不换颜色——强调靠位置和字号，不靠色块。
 */
@Composable
fun Card(
    modifier: Modifier = Modifier,
    emphasized: Boolean = false,
    onClick: (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    val bg = if (emphasized) Lamp.colors.surfaceHigh else Lamp.colors.surface
    val base = modifier
        .fillMaxWidth()
        .clip(RoundedCornerShape(Radius.CARD.dp))
        .background(bg)
        .border(1.dp, Lamp.colors.line, RoundedCornerShape(Radius.CARD.dp))
    Box(if (onClick != null) base.clickable(onClick = onClick) else base) {
        Column(Modifier.padding(16.dp)) { content() }
    }
}

/**
 * 桌面版主力的那种「图标 + 标题 + 说明 + 右侧控件」行。
 * 渠道、模型、设置三页全是它，所以在组件层收口，别处不再自己拼 Row。
 */
@Composable
fun SettingCard(
    icon: ImageVector,
    title: String,
    desc: String? = null,
    onClick: (() -> Unit)? = null,
    trailing: @Composable (() -> Unit)? = null,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(Radius.CARD.dp))
            .background(Lamp.colors.surface)
            .border(1.dp, Lamp.colors.line, RoundedCornerShape(Radius.CARD.dp))
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = if (onClick != null) Lamp.colors.amber else Lamp.colors.textDim,
            modifier = Modifier.size(20.dp),
        )
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(
                title,
                color = Lamp.colors.text,
                style = MaterialTheme.typography.titleMedium,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            if (desc != null) {
                Spacer(Modifier.height(2.dp))
                Text(
                    desc,
                    color = Lamp.colors.textDim,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
        if (trailing != null) {
            Spacer(Modifier.width(12.dp))
            trailing()
        }
    }
}

@Composable
fun SectionLabel(text: String) {
    Text(
        text,
        color = Lamp.colors.textDim,
        style = MaterialTheme.typography.titleMedium,
        modifier = Modifier.padding(start = 20.dp, top = 16.dp, bottom = 5.dp),
    )
}

/** 药丸：状态的最小单位。dot 决定颜色，文字跟着它。 */
@Composable
fun Pill(text: String, color: Color, dot: Boolean = true) {
    Row(
        Modifier
            .clip(RoundedCornerShape(Radius.PILL.dp))
            .background(color.copy(alpha = if (Lamp.colors.isDark) 0.14f else 0.10f))
            .border(1.dp, color.copy(alpha = 0.34f), RoundedCornerShape(Radius.PILL.dp))
            .padding(horizontal = 8.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (dot) {
            Box(
                Modifier
                    .size(6.dp)
                    .clip(RoundedCornerShape(Radius.PILL.dp))
                    .background(color),
            )
            Spacer(Modifier.width(5.dp))
        }
        Text(text, color = color, style = MaterialTheme.typography.labelSmall)
    }
}

/** 行式列表：一行一件事，高度统一，不画大卡（55 个模型必须能滚）。 */
@Composable
fun RowItem(
    title: String,
    subtitle: String? = null,
    trailing: @Composable (() -> Unit)? = null,
    leading: @Composable (() -> Unit)? = null,
    onClick: (() -> Unit)? = null,
    dense: Boolean = false,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 20.dp, vertical = if (dense) 10.dp else 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (leading != null) {
            leading()
            Spacer(Modifier.width(12.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(
                title,
                color = Lamp.colors.text,
                style = MaterialTheme.typography.titleMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (subtitle != null) {
                Spacer(Modifier.height(2.dp))
                Text(
                    subtitle,
                    color = Lamp.colors.textDim,
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (trailing != null) {
            Spacer(Modifier.width(10.dp))
            trailing()
        }
    }
}

@Composable
fun Divider() {
    Box(
        Modifier
            .fillMaxWidth()
            .padding(start = 20.dp, end = 20.dp)
            .height(1.dp)
            .background(Lamp.colors.line),
    )
}

/** 加载态：一行转圈，不占满屏。 */
@Composable
fun LoadingRow(text: String = "正在读取…") {
    Row(
        Modifier.fillMaxWidth().padding(24.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CircularProgressIndicator(
            Modifier.size(18.dp),
            color = Lamp.colors.amber,
            strokeWidth = 2.dp,
        )
        Spacer(Modifier.width(12.dp))
        Text(text, color = Lamp.colors.textDim, style = MaterialTheme.typography.bodyMedium)
    }
}

/**
 * 错态：网关原话必须完整的出现在这里。
 *
 * 桌面版用的是 InfoBar 形状（左侧一档真色底 + 描边）；手机上把动作也放进同一块
 * 里，因为网关没起的时候这是屏幕上唯一的可点区域。
 * `face.showStart` 为真（gateway_down）时才给「启动」——那是真的能自救的动作；
 * 业务拒绝（gateway_rejected）没有这个按钮，因为再点一次结果不会变。
 */
@Composable
fun ErrorBlock(err: UiError, onStart: (() -> Unit)? = null) {
    // 页面的左右基准是 20dp（卡片、分组标题都在这条线上）；错误块用 16 会
    // 比其它块窄 4dp，一眼看出是"另一套网格"。
    Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp)) {
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(Radius.CARD.dp))
                .background(Lamp.colors.bad.copy(alpha = if (Lamp.colors.isDark) 0.12f else 0.08f))
                .border(1.dp, Lamp.colors.bad.copy(alpha = 0.35f), RoundedCornerShape(Radius.CARD.dp))
                .padding(14.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(
                    Icons.Outlined.Lightbulb,
                    contentDescription = null,
                    tint = Lamp.colors.bad,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(10.dp))
                Text(
                    err.face.title,
                    color = Lamp.colors.bad,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                )
            }
            Spacer(Modifier.height(6.dp))
            // 网关原话，不翻译。
            Text(err.message, color = Lamp.colors.text, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(2.dp))
            Text(
                "code: ${err.code}",
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.labelSmall,
            )
            if (err.face.showStart && onStart != null) {
                Spacer(Modifier.height(12.dp))
                PrimaryButton("启动网关", onClick = onStart)
            }
        }
    }
}

/** 空态：一句说明 + 一个指向下一步的提示，不画插画。 */
@Composable
fun EmptyBlock(title: String, hint: String? = null) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp)) {
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(Radius.CARD.dp))
                .background(Lamp.colors.surface)
                .border(1.dp, Lamp.colors.line, RoundedCornerShape(Radius.CARD.dp))
                .padding(16.dp),
        ) {
            Text(title, color = Lamp.colors.textDim, style = MaterialTheme.typography.titleMedium)
            if (hint != null) {
                Spacer(Modifier.height(4.dp))
                Text(hint, color = Lamp.colors.textFaint, style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

// 按钮族在 Buttons.kt（一排按钮的高矮、圆角、状态层必须来自同一处）。

/** 黄条：只在「这件事是真的要用户处理」时出现（§6.4 兜底 / §6.8 Key 漂移）。 */
@Composable
fun WarnBar(text: String) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 6.dp)
            .clip(RoundedCornerShape(Radius.CARD.dp))
            .background(Lamp.colors.warn.copy(alpha = if (Lamp.colors.isDark) 0.14f else 0.12f))
            .border(1.dp, Lamp.colors.warn.copy(alpha = 0.32f), RoundedCornerShape(Radius.CARD.dp))
            .padding(12.dp),
    ) {
        Text(text, color = Lamp.colors.warn, style = MaterialTheme.typography.bodySmall)
    }
}

/**
 * 「标签 — 值」。标签 84dp 固定宽，值吃剩余宽度。
 *
 * 值是 URL / Key / 端口 / 版本这类东西时走 mono：等宽字体让 base URL 和 Key
 * 的前缀一眼对得齐，也避免 1 与 l 混读。非 mono 时用正文字号，不再一档降级。
 */
@Composable
fun KeyValue(key: String, value: String, mono: Boolean = false) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            key,
            color = Lamp.colors.textDim,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.width(84.dp),
        )
        Text(
            value,
            color = Lamp.colors.text,
            style = if (mono) {
                MaterialTheme.typography.bodySmall.copy(fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)
            } else {
                MaterialTheme.typography.bodyMedium
            },
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
    }
}


/**
 * Fluent 形状的输入框。
 *
 * 桌面版的输入框是「标签在左、框在右」的 SettingsCard 行，不是 Material 那种
 * 把标签浮在边框缺口上的 OutlinedTextField。手机上宽度不够并排，就退一档：
 * 标签在上、框在下，框本身用 surface + 细描边 + 4px 圆角，聚焦时才亮强调色边。
 * 全应用只此一个输入框形状，设置页、搜索框、登录弹层都用它。
 */
@Composable
fun LampField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    singleLine: Boolean = true,
    numeric: Boolean = false,
    keyboardActions: androidx.compose.foundation.text.KeyboardActions? = null,
) {
    val focused = remember { androidx.compose.runtime.mutableStateOf(false) }
    Column(modifier.fillMaxWidth()) {
        if (label != null) {
            Text(
                label,
                color = Lamp.colors.textDim,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(bottom = 5.dp),
            )
        }
        androidx.compose.foundation.text.BasicTextField(
            value = value,
            onValueChange = onValueChange,
            singleLine = singleLine,
            keyboardOptions = if (numeric) {
                androidx.compose.foundation.text.KeyboardOptions(
                    keyboardType = androidx.compose.ui.text.input.KeyboardType.Number,
                )
            } else {
                androidx.compose.foundation.text.KeyboardOptions()
            },
            keyboardActions = keyboardActions ?: androidx.compose.foundation.text.KeyboardActions(),
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = Lamp.colors.text),
            cursorBrush = androidx.compose.ui.graphics.SolidColor(Lamp.colors.amber),
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(Radius.CONTROL.dp))
                .background(Lamp.colors.surfaceHigh)
                .border(
                    width = 1.dp,
                    color = if (focused.value) Lamp.colors.amber else Lamp.colors.line,
                    shape = RoundedCornerShape(Radius.CONTROL.dp),
                )
                .padding(horizontal = 12.dp, vertical = 11.dp)
                .onFocusChanged { focused.value = it.isFocused },
            decorationBox = { inner ->
                Box {
                    if (value.isEmpty() && placeholder != null) {
                        Text(
                            placeholder,
                            color = Lamp.colors.textFaint,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    inner()
                }
            },
        )
    }
}

/**
 * Fluent 开关：药丸轨道 + 小圆钮，通高 20dp。
 *
 * Material 的 Switch 是 52×32、钮几乎填满轨道，视觉上比桌面版重一档。
 * 桌面版 WinUI 的 ToggleSwitch 是 40×20 的细轨道、钮 12，开口很窄——
 * 这正是「一眼认出是同一个产品」的细节之一，所以这里自己画。
 */
@Composable
fun LampSwitch(checked: Boolean, onChange: (Boolean) -> Unit, enabled: Boolean = true) {
    val t by androidx.compose.animation.core.animateFloatAsState(
        if (checked) 1f else 0f,
        label = "switch",
    )
    // Canvas 的绘制 lambda 不是 @Composable 上下文，Lamp.colors 必须在外面取好。
    val accent = Lamp.colors.amber
    val offTrack = Lamp.colors.textFaint.copy(alpha = 0.40f)
    val knob = Lamp.colors.onAmber
    val knobOff = Lamp.colors.surfaceSolid
    Box(
        Modifier
            .clip(RoundedCornerShape(Radius.PILL.dp))
            .clickable(enabled = enabled) { onChange(!checked) },
    ) {
        androidx.compose.foundation.Canvas(
            Modifier
                .size(width = 40.dp, height = 20.dp)
                .clip(RoundedCornerShape(Radius.PILL.dp)),
        ) {
            val r = size.height / 2
            val knobR = r - (3.5f).dp.toPx()
            drawRoundRect(
                color = if (checked) accent else offTrack,
                cornerRadius = androidx.compose.ui.geometry.CornerRadius(r, r),
            )
            drawCircle(
                color = if (checked) knob else knobOff,
                radius = knobR,
                center = androidx.compose.ui.geometry.Offset(
                    x = r + (size.width - 2 * r) * t,
                    y = r,
                ),
            )
        }
    }
}

/**
 * 大数字。一号字 + 一行说明，说明在上、数字在下会读反层级，
 * 所以数字先、标签后，标签一档小字且不提字重。
 */
@Composable
fun BigNumber(value: String, caption: String, modifier: Modifier = Modifier) {
    Column(modifier) {
        Text(
            value,
            color = Lamp.colors.text,
            style = MaterialTheme.typography.displayLarge,
            maxLines = 1,
        )
        Spacer(Modifier.height(2.dp))
        Text(caption, color = Lamp.colors.textDim, style = MaterialTheme.typography.bodySmall)
    }
}
