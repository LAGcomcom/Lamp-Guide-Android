package com.lampguide.lampnative.ui

import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsHoveredAsState
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.Radius

/**
 * 按钮族。形状照桌面版与 DSH 前端的同一套规格收口：
 *
 * · 一档高度（md 40 / sm 32），不靠内边距自己长出高低——导出一排按钮时高矮
 *   不齐是最先被看出来的粗糙。
 * · 圆角走 Radius.CONTROL（8），与输入框、卡片同一档；圆角混排是 P1。
 * · 底色 + 描边 + 悬停/按下三层，状态由 spring 过渡而不是写死时长：
 *   按下像"按进去了"，不是"过一会儿变色"。
 * · 全应用同时只允许一个实心强调按钮（Primary）。次动作 Outline，第三层动作
 *   Ghost，破坏性动作 Critical。图标与文字同色，单独给图标换色会把层级说乱。
 *
 * 旧的 `PrimaryButton` / `GhostButton` 保留为薄壳，调用点不用改。
 */
enum class ButtonTone { Primary, Outline, Ghost, Critical }

enum class ButtonSize(val h: Int, val padH: Int, val fontSp: Int) {
    Md(40, 16, 14),
    Sm(32, 12, 13),
}

@Composable
fun LampButton(
    text: String,
    onClick: () -> Unit,
    tone: ButtonTone = ButtonTone.Outline,
    size: ButtonSize = ButtonSize.Md,
    icon: ImageVector? = null,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    val c = Lamp.colors
    val source = remember { MutableInteractionSource() }
    val pressed by source.collectIsPressedAsState()
    val hovered by source.collectIsHoveredAsState()

    // 一层叠色表达状态：底 -> 按下(1.0) -> 悬停(0.45)。数值是弹簧目标，
    // 不是毫秒数，手感来自阻尼而不是计时器。
    val t by animateFloatAsState(
        targetValue = when {
            !enabled -> 0f
            pressed -> 1f
            hovered -> 0.45f
            else -> 0f
        },
        animationSpec = spring(dampingRatio = 0.72f, stiffness = Spring.StiffnessMediumLow),
        label = "button-state",
    )

    val base = when (tone) {
        ButtonTone.Primary -> if (enabled) c.amber else c.surfaceHigh
        ButtonTone.Critical -> if (enabled) c.bad else c.surfaceHigh
        ButtonTone.Outline -> Color.Transparent
        ButtonTone.Ghost -> Color.Transparent
    }
    val overlay = when (tone) {
        ButtonTone.Primary -> lerp(c.amber, c.onAmber, 0.18f)
        ButtonTone.Critical -> lerp(c.bad, Color.Black, 0.22f)
        // 透明底的两个层级靠"浮起一层"表达状态，不换色相。
        ButtonTone.Outline, ButtonTone.Ghost -> c.surfaceHigh
    }
    val fill = if (tone == ButtonTone.Primary || tone == ButtonTone.Critical) {
        lerp(base, overlay, t * 0.85f)
    } else {
        lerp(Color.Transparent, overlay, t * 0.9f)
    }

    val label = when {
        !enabled -> c.textFaint
        tone == ButtonTone.Primary -> c.onAmber
        tone == ButtonTone.Critical -> c.onBad
        else -> c.text
    }
    val stroke = when {
        tone == ButtonTone.Primary || tone == ButtonTone.Critical -> Color.Transparent
        !enabled -> c.line
        // 描边不按状态降 alpha：浅色主题下 lineStrong 本身只有 0.16 黑，
        // 再乘个 0.72 就掉到几乎看不见——按钮的边界是它唯一的存在感来源。
        // 悬停/按下靠底色变化表达，不靠描边变粗。
        else -> c.lineStrong
    }

    val shape = RoundedCornerShape(Radius.CONTROL.dp)
    Row(
        modifier
            .clip(shape)
            .background(fill)
            .border(1.dp, stroke, shape)
            .clickable(
                enabled = enabled,
                interactionSource = source,
                indication = null,
                onClick = onClick,
            )
            .defaultMinSize(minHeight = size.h.dp)
            .padding(horizontal = size.padH.dp)
            .height(size.h.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        if (icon != null) {
            Icon(
                icon,
                contentDescription = null,
                tint = label,
                modifier = Modifier.size(if (size == ButtonSize.Md) 16.dp else 14.dp),
            )
            Spacer(Modifier.width(6.dp))
        }
        Text(
            text,
            color = label,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = if (tone == ButtonTone.Primary) FontWeight.SemiBold else FontWeight.Normal,
            maxLines = 1,
        )
    }
}

/** 方图标按钮：只有图标、只有一格，行内右侧的 ＋ / ⋯ / ⚙ 用它。 */
@Composable
fun LampIconButton(
    icon: ImageVector,
    onClick: () -> Unit,
    contentDescription: String? = null,
    enabled: Boolean = true,
    size: Int = 36,
    modifier: Modifier = Modifier,
) {
    val c = Lamp.colors
    val source = remember { MutableInteractionSource() }
    val pressed by source.collectIsPressedAsState()
    val t by animateFloatAsState(
        targetValue = if (pressed) 1f else 0f,
        animationSpec = spring(dampingRatio = 0.7f, stiffness = Spring.StiffnessMediumLow),
        label = "icon-state",
    )
    Box(
        modifier
            .size(size.dp)
            .clip(RoundedCornerShape(Radius.CONTROL.dp))
            .background(lerp(Color.Transparent, c.surfaceHigh, t))
            .border(1.dp, if (enabled) c.lineStrong else c.line, RoundedCornerShape(Radius.CONTROL.dp))
            .clickable(enabled = enabled, interactionSource = source, indication = null, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            icon,
            contentDescription = contentDescription,
            tint = if (enabled) c.textDim else c.textFaint,
            modifier = Modifier.size(18.dp),
        )
    }
}

/**
 * 分段控件：一组互斥取值的"当前是哪一格"。
 *
 * 与按钮的区别在这里：按钮是"要发生一件事"，分段是"现在停在哪"。所以选中项
 * 不用实心强调色（那是主按钮的语法），用底层高亮 + 更实的描边，字色提一档。
 * 一排里只有一个选中项，也就不会跟屏上的主动作抢注意力。
 */
@Composable
fun Segmented(
    options: List<Pair<String, String>>,
    selected: String,
    onPick: (String) -> Unit,
    modifier: Modifier = Modifier,
    size: ButtonSize = ButtonSize.Sm,
) {
    val c = Lamp.colors
    Row(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        for ((value, label) in options) {
            val on = value == selected
            val source = remember { MutableInteractionSource() }
            val pressed by source.collectIsPressedAsState()
            val t by animateFloatAsState(
                targetValue = if (pressed) 1f else 0f,
                animationSpec = spring(dampingRatio = 0.75f, stiffness = Spring.StiffnessMediumLow),
                label = "seg-state",
            )
            Box(
                Modifier
                    .clip(RoundedCornerShape(Radius.CONTROL.dp))
                    .background(
                        if (on) c.surfaceHigh else lerp(Color.Transparent, c.surfaceHigh, t * 0.6f),
                    )
                    .border(
                        1.dp,
                        when {
                            on -> c.lineStrong
                            pressed -> c.line
                            else -> c.lineStrong
                        },
                        RoundedCornerShape(Radius.CONTROL.dp),
                    )
                    .clickable(interactionSource = source, indication = null) { onPick(value) }
                    .defaultMinSize(minHeight = size.h.dp)
                    .padding(horizontal = size.padH.dp)
                    .height(size.h.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    label,
                    color = if (on) c.text else c.textDim,
                    style = MaterialTheme.typography.bodySmall,
                    fontWeight = if (on) FontWeight.Medium else FontWeight.Normal,
                    maxLines = 1,
                )
            }
        }
    }
}

// 三个薄壳都把 onClick 放在最后一个参数：全工程的调用点写的是
// "GhostButton("保存") { … }"，尾随 lambda 必须落到 onClick 上。

/** 主按钮：屏上只此一个实心强调色的动作。 */
@Composable
fun PrimaryButton(
    text: String,
    enabled: Boolean = true,
    icon: ImageVector? = null,
    onClick: () -> Unit,
) = LampButton(text, onClick, ButtonTone.Primary, ButtonSize.Md, icon, enabled)

/** 次按钮：描边 + 透明底，复制 / 轮换 / 保存 这类"另一条路"。 */
@Composable
fun GhostButton(
    text: String,
    enabled: Boolean = true,
    icon: ImageVector? = null,
    onClick: () -> Unit,
) = LampButton(text, onClick, ButtonTone.Outline, ButtonSize.Sm, icon, enabled)

/** 第三层：无描边的纯文字动作，用于行内低频操作。 */
@Composable
fun SubtleButton(
    text: String,
    enabled: Boolean = true,
    onClick: () -> Unit,
) = LampButton(text, onClick, ButtonTone.Ghost, ButtonSize.Sm, null, enabled)
