package com.lampguide.lampnative.ui.screens

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.DayBar
import com.lampguide.lampnative.data.Mappers
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.ui.Card
import com.lampguide.lampnative.ui.Col
import com.lampguide.lampnative.ui.DataTable
import com.lampguide.lampnative.ui.EmptyBlock
import com.lampguide.lampnative.ui.ErrorBlock
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.LoadingRow
import com.lampguide.lampnative.ui.ScreenScaffold
import com.lampguide.lampnative.ui.TableCard
import com.lampguide.lampnative.ui.theme.Fmt
import com.lampguide.lampnative.ui.theme.Lamp

/**
 * S5 用量。目的：今天/近 7 天烧了多少，哪个模型最贵。
 *
 * 数据：`state.stats.days` + `state.stats.recent`。
 * 形态：原生 Canvas 7 天柱（不引图表库）+ 按 token 降序 5 行。
 */
@Composable
fun S5Usage(load: Load<StateView>) {
    ScreenScaffold(title = "用量", subtitle = "近 7 天") {
        when (load) {
            Load.Loading -> LoadingRow()
            is Load.Error -> ErrorBlock(load.err)
            is Load.Empty -> EmptyBlock("今天还没有请求")
            is Load.Data -> S5Body(load.value)
        }
    }
}

@Composable
private fun S5Body(s: StateView) {
    val todayKey = s.now.take(10).ifBlank { "" }
    val bars = runCatching { Mappers.sevenDays(s.stats, todayKey) }.getOrDefault(emptyList())
    val top = runCatching { Mappers.topModels(s.stats, todayKey) }.getOrDefault(emptyList())
    val total7 = bars.sumOf { it.output.toLong() }
    val today = Mappers.today(s.stats, todayKey)

    if (bars.all { it.output == 0 } && today.requests == 0) {
        EmptyBlock("最近 7 天还没有请求", "接上客户端之后这里会有数据")
        return
    }

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 8.dp)
            .padding(bottom = 88.dp),
    ) {
        Card(emphasized = true) {
            Text("近 7 天输出 token", color = Lamp.colors.textDim, style = MaterialTheme.typography.labelSmall)
            Text(
                Fmt.tokens(total7),
                color = Lamp.colors.amber,
                style = MaterialTheme.typography.displayLarge,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                "今天 ${Fmt.tokens(today.output)} token · ${today.requests} 次请求 · 失败 ${today.failed}",
                color = Lamp.colors.textDim,
                style = MaterialTheme.typography.bodySmall,
            )
            Spacer(Modifier.height(14.dp))
            SevenDayBars(bars)
        }

        // 按模型花的钱：五列对齐，数字列右对齐才有可比性——一行行写
        // "1.2k token · 20 次" 时，两行的数位是对不上的。
        Spacer(Modifier.height(12.dp))
        if (top.isEmpty()) {
            EmptyBlock("最近 7 天没有按模型的数据")
        } else {
            TableCard(title = "最贵的 5 个模型") {
                DataTable(
                    // 表头字数决定列宽下限：手机 411dp 里"输出 token"已经占满一档，
                    // "请求"再窄就只剩省略号——改成两个字的"次"，一列就够。
                    cols = listOf(
                        Col("模型", 2.15f),
                        Col("输出", 1.05f, numeric = true),
                        Col("次", 0.55f, numeric = true),
                        Col("均次", 0.7f, numeric = true),
                    ),
                    rows = top.map { m ->
                        listOf(
                            m.id,
                            Fmt.tokens(m.tokens),
                            m.requests.toString(),
                            if (m.requests > 0) Fmt.tokens(m.tokens / m.requests) else "—",
                        )
                    },
                )
            }
        }

        if (s.stats.recent.isNotEmpty()) {
            Spacer(Modifier.height(12.dp))
            TableCard(title = "最近调用") {
                DataTable(
                    cols = listOf(
                        Col("模型", 1.9f),
                        Col("入", 0.66f, numeric = true),
                        Col("出", 0.66f, numeric = true),
                        Col("首字", 0.72f, numeric = true),
                        Col("时间", 0.72f, numeric = true),
                    ),
                    rows = s.stats.recent.take(6).map { r ->
                        listOf(
                            r.model.ifBlank { "—" },
                            Fmt.tokens(r.input),
                            Fmt.tokens(r.output),
                            if (r.ttftMs > 0) Fmt.ms(r.ttftMs) else "—",
                            if (r.at > 0) Fmt.clock(r.at) else "—",
                        )
                    },
                )
            }
        }
    }
}

/**
 * 7 天柱：原生 Canvas，一根柱一个日，最高的那根用琥珀，其余用琥珀的暗色。
 * 值取 `days[date].output`，不做任何估算。
 */
@Composable
private fun SevenDayBars(bars: List<DayBar>) {
    val amber = Lamp.colors.amber
    val faint = Lamp.colors.amber.copy(alpha = 0.35f)
    val labels = Lamp.colors.textFaint
    val max = (bars.maxOfOrNull { it.output } ?: 0).coerceAtLeast(1)

    Column {
        Canvas(Modifier.fillMaxWidth().height(120.dp)) {
            drawBars(bars, max, amber, faint)
        }
        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            for (b in bars) {
                Text(b.label, color = labels, style = MaterialTheme.typography.labelSmall)
            }
        }
    }
}

private fun DrawScope.drawBars(bars: List<DayBar>, max: Int, amber: androidx.compose.ui.graphics.Color, faint: androidx.compose.ui.graphics.Color) {
    if (bars.isEmpty()) return
    val gap = size.width / bars.size
    val barW = gap * 0.52f
    val baseline = size.height
    bars.forEachIndexed { i, b ->
        val h = if (max <= 0) 0f else (b.output.toFloat() / max) * (size.height * 0.82f)
        val x = gap * i + (gap - barW) / 2
        val color = if (b.output == max && max > 0) amber else faint
        drawRoundRect(
            color = color,
            topLeft = Offset(x, baseline - h.coerceAtLeast(if (b.output > 0) 2f else 0f)),
            size = Size(barW, h.coerceAtLeast(if (b.output > 0) 2f else 0f)),
            cornerRadius = CornerRadius(barW / 3, barW / 3),
        )
    }
}
