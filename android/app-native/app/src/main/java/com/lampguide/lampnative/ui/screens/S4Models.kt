package com.lampguide.lampnative.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.AggregateState
import com.lampguide.lampnative.data.Mappers
import com.lampguide.lampnative.data.ModelRow
import com.lampguide.lampnative.data.ModelState
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.ui.Col
import com.lampguide.lampnative.ui.EmptyBlock
import com.lampguide.lampnative.ui.ErrorBlock
import com.lampguide.lampnative.ui.GhostButton
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.LoadingRow
import com.lampguide.lampnative.ui.Pill
import com.lampguide.lampnative.ui.ScreenScaffold
import com.lampguide.lampnative.ui.Segmented
import com.lampguide.lampnative.ui.SubtleButton
import com.lampguide.lampnative.ui.TableDivider
import com.lampguide.lampnative.ui.TableHeader
import com.lampguide.lampnative.ui.theme.Fmt
import com.lampguide.lampnative.ui.theme.Lamp

/**
 * S4 模型。目的：找一个「现在能用、额度没烧完」的模型。
 *
 * **行式，不是卡**：55 个模型必须能滚能搜，一张带描述的大卡会让手指划不到
 * 底。多路可用时右上角给 `×N` 角标，N 取 `aggregate/state` 的候选长度。
 */
private enum class Filter(val label: String) {
    Available("可用"),
    All("全部"),
    WithQuota("有额度"),
}

@Composable
fun S4Models(
    load: Load<StateView>,
    agg: Load<AggregateState>,
    onProbe: (String, String) -> Unit,
    onHide: (String, Boolean) -> Unit,
    /**
     * 一次把所有「待测」的模型挨个测一遍。
     *
     * 自建渠道（Qoder / WorkBuddy / NVIDIA / ModelScope / Intern）的模型在网关
     * 侧没有自动探测轮，只有用户手动点「测一下」才会拿到真实状态。上百条靠一行
     * 一行点不现实，所以给一个整批入口。
     */
    onProbePending: (List<Pair<String, String>>) -> Unit = {},
) {
    var query by remember { mutableStateOf("") }
    // 默认落在「可用」上；但网关刚起来、一个都没探到的时候，「可用」是一张空表，
    // 用户点进来只会以为自己什么都没接。那时先给「全部」，让他看得见目录里有东西。
    val anyAvailable = (load as? Load.Data)?.let { Mappers.availableCount(it.value.models) > 0 } ?: true
    var filter by remember(anyAvailable) { mutableStateOf(if (anyAvailable) Filter.Available else Filter.All) }

    ScreenScaffold(
        title = "模型",
        subtitle = if (load is Load.Data) {
            val models = load.value.models
            val unprobed = Mappers.unprobedCount(models)
            val pending = Mappers.pendingCount(models)
            buildString {
                append("可用 ${Mappers.availableCount(models)} / 共 ${models.size}")
                // 「待测」和「还没探测」是两件事：前者是你自己加的渠道（Qoder、
                // WorkBuddy、NVIDIA…）里网关还没跑过的，后者是免费车道里还没轮到的。
                // 分开报，用户才知道该点哪一行的「测一下」。
                if (pending > 0) append(" · $pending 个待测")
                if (unprobed > 0) append(" · $unprobed 个还没探测")
            }
        } else {
            null
        },
    ) {
        when (load) {
            Load.Loading -> LoadingRow()
            is Load.Error -> ErrorBlock(load.err)
            is Load.Empty -> EmptyBlock("网关没有报出任何模型", "去渠道页看看账号是不是掉了")
            is Load.Data -> {
                val all = load.value.models
                val aggValue = (agg as? Load.Data)?.value
                Column(Modifier.fillMaxWidth()) {
                    SearchBar(query) { query = it }
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = 20.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        FilterChips(filter) { filter = it }
                        Spacer(Modifier.weight(1f))
                        // 只在真的有待测项时出现：没有待测还摆一个「全部测一遍」，
                        // 等于让用户拿一次批量上游请求去换一句"没有可测的"。
                        // 传 (id, channel) 而不是裸 id：同名模型在不同来源各有一条，
                        // 只带 id 会探到错误的那一条。
                        val pendingIds = remember(load) {
                            (load as? Load.Data)?.value?.models
                                ?.filter { ModelState.of(it.state) == ModelState.Custom && !it.hidden }
                                ?.map { it.id to it.channel }
                                .orEmpty()
                        }
                        if (pendingIds.isNotEmpty()) {
                            SubtleButton("测全部 ${pendingIds.size}") { onProbePending(pendingIds) }
                        }
                    }
                    val shown = all.filter { row ->
                        val byName = query.isBlank() ||
                            row.id.contains(query, true) ||
                            row.name.contains(query, true)
                        val st = ModelState.of(row.state)
                        byName && when (filter) {
                            // 「可用」含着自建渠道的「待测」：那是网关还没替它测，
                            // 不是不可用。不并进来，用户在 Qoder / WorkBuddy 里加过的
                            // DeepSeek 就会从默认视图里整个消失，看上去像"没有这个模型"。
                            Filter.Available ->
                                st == ModelState.Available || st == ModelState.Custom
                            Filter.All -> true
                            Filter.WithQuota -> st == ModelState.Available && row.quotaUsed > 0
                        }
                    }
                    /*
                     * 排序：真可用在前，其次待测，其余保持网关自己的目录顺序。
                     *
                     * 自建渠道（Qoder / WorkBuddy / NVIDIA / ModelScope / Intern）在
                     * 网关侧排在免费车道之后，479 行里它们是最后十几条；不分档的话
                     * "我加过的模型"永远要滑到底才看得见。但也不能把它们排到最前——
                     * 那会把 14 个真能用的挤下去，页面第一屏全是「待测」，同样答不了
                     * "现在能用什么"。
                     */
                    val ordered = shown.sortedWith(
                        compareBy { row ->
                            when (ModelState.of(row.state)) {
                                ModelState.Available -> 0
                                ModelState.Custom -> 1
                                else -> 2
                            }
                        },
                    )
                    if (shown.isEmpty()) {
                        EmptyBlock(
                            if (all.isEmpty()) "网关没有报出任何模型"
                            else if (query.isNotBlank()) "没有匹配「$query」的模型"
                            else "还没有模型探到可用状态。切到「全部」看目录，或等这一轮探测跑完。",
                        )
                    } else {
                        // 表头固定在滚动区外面：55 行往下滑到底时，"这一列是什么"
                        // 必须还在屏幕顶上，否则右列那串数字是 token 还是次数就没人知道。
                        Column(Modifier.padding(horizontal = 20.dp)) {
                            TableHeader(
                                cols = listOf(
                                    Col("模型", 2.0f),
                                    Col("首字", 0.62f, numeric = true),
                                    Col("上下文", 0.72f, numeric = true),
                                    Col("今日", 0.68f, numeric = true),
                                ),
                            )
                        }
                        LazyColumn(Modifier.fillMaxWidth().padding(bottom = 88.dp)) {
                            // key 必须带上 channel：免费车道和 WorkBuddy 都会报
                            // deepseek-v4.1-flash，只按 id 做 key 会撞成重复键
                            // （LazyColumn 直接抛 IllegalArgumentException）。
                            items(ordered, key = { "${it.channel}/${it.id}" }) { m ->
                                ModelLine(
                                    m = m,
                                    candidates = aggValue?.let { Mappers.candidatesOf(it, m.id) },
                                    onProbe = { onProbe(m.id, m.channel) },
                                    onHide = { onHide(m.id, !m.hidden) },
                                )
                                TableDivider()
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchBar(value: String, onChange: (String) -> Unit) {
    com.lampguide.lampnative.ui.LampField(
        value = value,
        onValueChange = onChange,
        placeholder = "按 id 或名字搜",
        modifier = Modifier.padding(horizontal = 20.dp, vertical = 6.dp),
    )
}

/**
 * 分段筛选。选中项不是"实心强调色药丸"——那是按钮语法，用在筛选上会跟屏里
 * 真正的主动作抢注意力。这里用底层高亮 + 描边表达"当前这一格"，字色只提一档。
 */
@Composable
private fun FilterChips(current: Filter, onPick: (Filter) -> Unit) {
    // 横向留白交给外层 Row：这里再补一份，筛选条会比表头往右缩一截。
    Segmented(
        options = Filter.entries.map { it.name to it.label },
        selected = current.name,
        onPick = { name -> onPick(Filter.valueOf(name)) },
        modifier = Modifier.padding(vertical = 8.dp),
    )
}

@Composable
private fun ModelLine(
    m: ModelRow,
    candidates: Int?,
    onProbe: () -> Unit,
    onHide: () -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .clickable { menu = !menu }
                .padding(start = 20.dp, end = 20.dp, top = 10.dp, bottom = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // 第一列：名字 + 多路角标 + 隐藏标；第二列起是可比的数量。
            Column(Modifier.weight(2.0f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        m.name.ifBlank { m.id },
                        color = if (m.hidden) Lamp.colors.textFaint else Lamp.colors.text,
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Medium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    if (candidates != null && candidates > 1) {
                        Spacer(Modifier.width(5.dp))
                        Text(
                            "×$candidates",
                            color = Lamp.colors.amber,
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.SemiBold,
                        )
                    }
                    if (m.hidden) {
                        Spacer(Modifier.width(5.dp))
                        Text("已隐藏", color = Lamp.colors.textFaint, style = MaterialTheme.typography.labelSmall)
                    }
                }
                // 副标题：来源 + 网关原话。来源是必须写的——同一个名字在免费
                // 车道和 WorkBuddy 里都有，不写来源时两行长得一模一样，用户没法
                // 判断点哪一条。
                val parts = buildList {
                    if (m.source.isNotBlank()) add(m.source)
                    val d = Mappers.modelSubtitle(m)
                    if (d.isNotBlank()) add(d)
                }
                if (parts.isNotEmpty()) {
                    Text(
                        parts.joinToString(" · "),
                        color = Lamp.colors.textDim,
                        style = MaterialTheme.typography.labelSmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            NumCell(Mappers.ttftOf(m), 0.62f)
            NumCell(Fmt.context(m.contextWindow), 0.72f)
            NumCell(Mappers.quotaOf(m), 0.68f)
            Spacer(Modifier.width(10.dp))
            StatePill(ModelState.of(m.state))
        }
        if (menu) {
            Row(
                Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, bottom = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                GhostButton("测一下", onClick = { menu = false; onProbe() })
                GhostButton(if (m.hidden) "显示" else "隐藏", onClick = { menu = false; onHide() })
            }
            if (m.detail.isNotBlank()) {
                Text(
                    m.detail,
                    color = Lamp.colors.textFaint,
                    style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.padding(start = 20.dp, end = 20.dp, bottom = 10.dp),
                )
            }
        }
    }
}

/** 数字格：右对齐、次一级色，宽度按权重分，和表头对齐。 */
@Composable
private fun androidx.compose.foundation.layout.RowScope.NumCell(text: String, weight: Float) {
    Text(
        text,
        color = Lamp.colors.textDim,
        style = MaterialTheme.typography.labelSmall,
        textAlign = TextAlign.End,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.weight(weight).padding(horizontal = 5.dp),
    )
}

@Composable
private fun StatePill(st: ModelState) {
    val color = when (st) {
        ModelState.Available -> Lamp.colors.ok
        ModelState.Throttled -> Lamp.colors.warn
        ModelState.RegionBlocked -> Lamp.colors.bad
        ModelState.Unavailable -> Lamp.colors.bad
        ModelState.Custom -> Lamp.colors.amber
        ModelState.Unknown -> Lamp.colors.textFaint
    }
    Pill(st.label, color)
}
