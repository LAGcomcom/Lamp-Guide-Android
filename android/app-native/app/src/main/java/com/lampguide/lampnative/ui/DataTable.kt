package com.lampguide.lampnative.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.Radius

/**
 * 数据表。规格照桌面版与 DSH 前端同一套：
 *
 * · 表头一档次级色 + 半粗，行体一档；只画 1dp/0.5 档横线，不画竖线——手机屏窄，
 *   竖线把每列挤成一条窄柱，读起来比不画更碎。
 * · 数字列右对齐，一串 token 的数位能上下对齐；文本列左对齐。
 * · 行高统一，靠 padding 撑；首列不留左内边距、末列不留右内边距。
 * · 不做横向滚动（手机没有这个习惯）：超宽靠列权重截断加省略号。
 *
 * 表与卡片列表是两件事：卡是一件事，表让同类字段一列一列对齐可比。
 * 行式列表要自己撑住滚动时，用 [TableHeader] + [TableRow] 直接拼进 LazyColumn。
 */
data class Col(
    val title: String,
    /** 权重；给 [widthDp] 时按固定宽度走。 */
    val weight: Float = 1f,
    val widthDp: Int? = null,
    val numeric: Boolean = false,
)

/**
 * 表头。
 *
 * [trailingWidth] 必须和 [TableRow] 传的一致：行尾若有固定小件（状态药丸、＋、
 * 退出），那段宽度不参与分列。表头不让位的话，表头按满宽分列、行体按扣除后分列，
 * 同一张表上下两行的列边界会差一个按钮的宽度，看起来"表头飘在外面"。
 */
@Composable
fun TableHeader(
    cols: List<Col>,
    modifier: Modifier = Modifier,
    trailingWidth: Int = 0,
) {
    Column(modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
            cols.forEachIndexed { i, col ->
                Text(
                    col.title,
                    color = Lamp.colors.textDim,
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.SemiBold,
                    textAlign = if (col.numeric) TextAlign.End else TextAlign.Start,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = cell(col, i, cols.lastIndex).padding(top = 2.dp, bottom = 7.dp),
                )
            }
            if (trailingWidth > 0) Spacer(Modifier.width(trailingWidth.dp))
        }
        Box(Modifier.fillMaxWidth().height(1.dp).background(Lamp.colors.line))
    }
}

/**
 * 一行。首列是主角：颜色与字重都比其余列高一级；其余列一档次级色。
 * [trailing] 是行尾的固定小件（状态药丸、＋ 按钮），不占列宽。
 */
@Composable
fun TableRow(
    cols: List<Col>,
    cells: List<String>,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    trailingWidth: Int = 0,
    trailing: @Composable (() -> Unit)? = null,
) {
    Row(
        modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        cells.forEachIndexed { i, text ->
            val col = cols.getOrElse(i) { Col("") }
            Text(
                text,
                color = if (i == 0) Lamp.colors.text else Lamp.colors.textDim,
                style = if (i == 0) MaterialTheme.typography.bodyMedium
                else MaterialTheme.typography.bodySmall,
                fontWeight = if (i == 0) FontWeight.Medium else FontWeight.Normal,
                textAlign = if (col.numeric) TextAlign.End else TextAlign.Start,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = cell(col, i, cols.lastIndex),
            )
        }
        if (trailing != null) {
            // 行尾固定宽度：表头用同一个数字让位，两边列边界才对得上。
            Spacer(Modifier.width(10.dp))
            Box(
                Modifier
                    .let { if (trailingWidth > 0) it.width((trailingWidth - 10).dp) else it },
                contentAlignment = Alignment.CenterEnd,
            ) { trailing() }
        }
    }
}

/**
 * 行线。浅色主题下 line 只有 0.058 黑，再降 alpha 就断线、表格会散，
 * 所以两个底色都用同一档实线，不按"深浅"再分两级。
 */
@Composable
fun TableDivider() {
    Box(
        Modifier
            .fillMaxWidth()
            .height(1.dp)
            .background(Lamp.colors.line),
    )
}

@Composable
fun DataTable(
    cols: List<Col>,
    rows: List<List<String>>,
    modifier: Modifier = Modifier,
    emptyText: String? = null,
    onRowClick: ((Int) -> Unit)? = null,
) {
    Column(modifier.fillMaxWidth()) {
        TableHeader(cols)
        if (rows.isEmpty()) {
            Text(
                emptyText ?: "—",
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(vertical = 14.dp),
            )
            return@Column
        }
        rows.forEachIndexed { r, cells ->
            TableRow(cols, cells, onClick = onRowClick?.let { cb -> { cb(r) } })
            if (r != rows.lastIndex) TableDivider()
        }
    }
}

/** 单元格：固定宽度优先，否则按权重分；首列无左距、末列无右距。 */
@Composable
private fun RowScope.cell(col: Col, index: Int, lastIndex: Int): Modifier {
    val fixed = col.widthDp
    val size = if (fixed != null) Modifier.width(fixed.dp) else Modifier.weight(col.weight)
    return size.padding(
        start = if (index == 0) 0.dp else 10.dp,
        end = if (index == lastIndex) 0.dp else 10.dp,
    )
}

/** 键值行：设置页/详情页的「标签 — 值」对齐，不画线，靠行距分组。 */
@Composable
fun KvRow(
    key: String,
    value: String,
    keyWidth: Int = 96,
    trailing: @Composable (() -> Unit)? = null,
) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            key,
            color = Lamp.colors.textDim,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.width(keyWidth.dp),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            value,
            color = Lamp.colors.text,
            style = MaterialTheme.typography.bodySmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (trailing != null) {
            Spacer(Modifier.width(8.dp))
            trailing()
        }
    }
}

/** 表容器：卡面 + 统一内边距，让表和页面上别的块对齐到同一条网格。 */
@Composable
fun TableCard(
    modifier: Modifier = Modifier,
    title: String? = null,
    content: @Composable () -> Unit,
) {
    Column(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(Radius.CARD.dp))
            .background(Lamp.colors.surface)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        if (title != null) {
            Text(
                title,
                color = Lamp.colors.text,
                style = MaterialTheme.typography.titleMedium,
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        content()
    }
}
