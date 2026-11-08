package com.lampguide.lampnative.ui.screens

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.ChannelRow
import com.lampguide.lampnative.data.ChannelState
import com.lampguide.lampnative.data.CheckinView
import com.lampguide.lampnative.data.Mappers
import com.lampguide.lampnative.ui.Card
import com.lampguide.lampnative.ui.Col
import com.lampguide.lampnative.ui.Divider
import com.lampguide.lampnative.ui.EmptyBlock
import com.lampguide.lampnative.ui.ErrorBlock
import com.lampguide.lampnative.ui.LampIconButton
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.LoadingRow
import com.lampguide.lampnative.ui.Pill
import com.lampguide.lampnative.ui.PrimaryButton
import com.lampguide.lampnative.ui.ScreenScaffold
import com.lampguide.lampnative.ui.SubtleButton
import com.lampguide.lampnative.ui.TableDivider
import com.lampguide.lampnative.ui.TableHeader
import com.lampguide.lampnative.ui.TableRow
import com.lampguide.lampnative.ui.WarnBar
import com.lampguide.lampnative.ui.theme.Fmt
import com.lampguide.lampnative.ui.theme.Lamp

/**
 * S2 渠道与签到。
 *
 * 目的：今天哪些账号还能用、签到跑了没有。
 * 数据来源：`channel/state` + `checkin/state`。
 */
@Composable
fun S2Channels(
    channels: Load<ChannelState>,
    checkin: Load<CheckinView>,
    onRunCheckin: () -> Unit,
    onAddAccount: (String) -> Unit,
    onLogout: (String, String) -> Unit,
    /** 展开第一家有账号的渠道。只给截图取证用——真实界面默认全部收着。 */
    openFirst: Boolean = false,
) {
    ScreenScaffold(title = "渠道与签到", subtitle = "今天哪些账号还能用") {
        Column(Modifier.fillMaxWidth()) {
            CheckinBar(checkin, onRunCheckin)
            when (channels) {
                Load.Loading -> LoadingRow("正在读取渠道…")
                is Load.Error -> ErrorBlock(channels.err)
                is Load.Empty -> EmptyBlock("还没有任何账号", "点右上角的 ＋ 添加一个账号")
                is Load.Data -> ChannelList(channels.value, onAddAccount, onLogout, openFirst)
            }
        }
    }
}

@Composable
private fun CheckinBar(load: Load<CheckinView>, onRun: () -> Unit) {
    Column(Modifier.padding(horizontal = 20.dp, vertical = 8.dp)) {
        when (load) {
            Load.Loading -> LoadingRow("正在读取签到状态…")
            is Load.Error -> ErrorBlock(load.err)
            is Load.Empty -> EmptyBlock("没有可签到的账号")
            is Load.Data -> {
                val v = load.value
                val rows = v.accounts
                val done = rows.count { Mappers.checkin(it, v).done }
                Card(emphasized = true) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text(
                                "今天签到 $done / ${rows.size}",
                                color = Lamp.colors.text,
                                style = MaterialTheme.typography.titleMedium,
                            )
                            val last = rows.maxOfOrNull { it.result.updatedAt } ?: 0L
                            Text(
                                if (v.running) "正在执行本轮…" else "最后一轮 ${Fmt.stamp(last)}",
                                color = Lamp.colors.textDim,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        if (v.running) {
                            CircularProgressIndicator(
                                Modifier.size(22.dp),
                                color = Lamp.colors.amber,
                                strokeWidth = 2.dp,
                            )
                        } else {
                            PrimaryButton("立即签到", onClick = onRun)
                        }
                    }
                    if (v.error.isNotBlank()) {
                        Spacer(Modifier.height(8.dp))
                        Text(v.error, color = Lamp.colors.bad, style = MaterialTheme.typography.bodySmall)
                    }
                    if (v.timezone.isNotBlank()) {
                        Spacer(Modifier.height(6.dp))
                        Text(
                            "自动签到 ${if (v.settings.enabled) "开" else "关"} · ${v.timezone} · 时点 ${
                                v.settings.times.joinToString(" / ").ifBlank { "—" }
                            }",
                            color = Lamp.colors.textFaint,
                            style = MaterialTheme.typography.labelSmall,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ChannelList(
    state: ChannelState,
    onAdd: (String) -> Unit,
    onLogout: (String, String) -> Unit,
    openFirst: Boolean = false,
) {
    val first = if (openFirst) {
        state.channels.firstOrNull { it.accountList.isNotEmpty() }?.id
    } else null
    val expanded = remember {
        mutableStateOf(if (first != null) setOf(first) else emptySet())
    }
    // 底部留白必须高过悬浮胶囊（56 高 + 离底 16），否则最后一家渠道压在导航下面。
    LazyColumn(Modifier.fillMaxWidth().padding(bottom = 88.dp)) {
        if (state.totals.expiringSoon > 0) {
            item {
                WarnBar("${state.totals.expiringSoon} 家渠道的额度在三天内作废，去下面删掉不要的账号或换新账号。")
            }
        }
        items(state.channels, key = { it.id }) { ch ->
            ChannelBlock(
                ch = ch,
                open = ch.id in expanded.value,
                onToggle = {
                    expanded.value = if (ch.id in expanded.value) {
                        expanded.value - ch.id
                    } else {
                        expanded.value + ch.id
                    }
                },
                onAdd = { onAdd(ch.id) },
                onLogout = { key -> onLogout(ch.id, key) },
            )
            Divider()
        }
    }
}

@Composable
private fun ChannelBlock(
    ch: ChannelRow,
    open: Boolean,
    onToggle: () -> Unit,
    onAdd: () -> Unit,
    onLogout: (String) -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        Row(
            Modifier
                .fillMaxWidth()
                .clickable(onClick = onToggle)
                .padding(start = 20.dp, end = 20.dp, top = 14.dp, bottom = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        ch.name.ifBlank { ch.id },
                        color = Lamp.colors.text,
                        style = MaterialTheme.typography.titleMedium,
                    )
                    Spacer(Modifier.width(8.dp))
                    Pill(
                        if (ch.loggedIn) "${ch.accountCount} 个账号" else "未登录",
                        if (ch.loggedIn) Lamp.colors.ok else Lamp.colors.textFaint,
                    )
                }
                // 副标题没有内容时不留一行「—」：一行只有破折号的空壳行，
                // 比少一行更让人以为数据没读到。
                val sub = subtitleOf(ch)
                if (sub.isNotBlank()) {
                    Text(
                        sub,
                        color = Lamp.colors.textDim,
                        style = MaterialTheme.typography.bodySmall,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            LampIconButton(
                icon = Icons.Outlined.Add,
                contentDescription = "添加 ${ch.name.ifBlank { ch.id }} 的账号",
                onClick = onAdd,
                size = 34,
            )
        }

        if (open) {
            if (ch.accountList.isEmpty()) {
                Row(Modifier.padding(start = 20.dp, end = 20.dp, bottom = 12.dp)) {
                    Text(
                        "这一家还没有账号。点 ＋ 登录或粘贴 Key。",
                        color = Lamp.colors.textFaint,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
            // 账号是一张表：名字、额度、来源三列对齐。写成一行行"名字 + 小字"时，
            // 第二个账号的额度比第一个长一截，右边缘就乱了，也看不出哪一列是钱。
            if (ch.accountList.isNotEmpty()) {
                Column(Modifier.padding(start = 32.dp, end = 20.dp, top = 2.dp, bottom = 10.dp)) {
                    // 额度这一列是"还剩 3 天 / 永久额度 / 1.2M"混排，右对齐会让
                    // 中文尾巴参差，所以按文本列左对齐；数字本身另有 mono 需求时再拆列。
                    val cols = listOf(
                        Col("账号", 1.55f),
                        Col("额度", 1.15f),
                        Col("来源", 0.8f),
                    )
                    // 72dp 给行尾的「退出」按钮让位，表头与行体用同一个数字。
                    TableHeader(cols = cols, trailingWidth = 72)
                    ch.accountList.forEachIndexed { i, a ->
                        val q = a.quota
                        TableRow(
                            cols = cols,
                            trailingWidth = 72,
                            cells = listOf(
                                a.label.ifBlank { a.nickname.ifBlank { a.id.ifBlank { "账号" } } },
                                when {
                                    a.expiresAtMs > 0 -> Fmt.remaining(a.expiresAtMs)
                                    q != null && q.detail.isNotBlank() -> q.detail
                                    else -> "—"
                                },
                                a.source.ifBlank { "—" },
                            ),
                            trailing = {
                                SubtleButton("退出", onClick = { onLogout(a.key) })
                            },
                        )
                        if (i != ch.accountList.lastIndex) TableDivider()
                    }
                }
            }
            // 「这一家怎么登录」放在账号表下面当脚注：折叠态只有一行，说了就被截断；
            // 展开态里它是解释"为什么只有 ＋ 没有别的动作"的那句话，属于表尾不属于表头。
            if (ch.login.kind.isNotBlank() || ch.login.note.isNotBlank()) {
                Text(
                    buildString {
                        append("登录方式 ")
                        append(ch.login.kind.ifBlank { "未知" })
                        if (ch.login.note.isNotBlank()) append(" · ").append(ch.login.note)
                    },
                    color = Lamp.colors.textFaint,
                    style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 2.dp, bottom = 6.dp),
                )
            }
            if (ch.catalogError.isNotBlank()) {
                WarnBar("${ch.name.ifBlank { ch.id }} 的模型目录读取失败：${ch.catalogError}")
            }
            Spacer(Modifier.height(6.dp))
        }
    }
}

/**
 * 渠道行副标题：只留两件影响判断的事——厂商是谁、额度还剩多少。
 *
 * 原来把"登录方式 paste"也拼进来，一行挤三段就没一段读得全，被截成「登录…」；
 * 登录方式在展开态里跟着 ＋ 按钮旁边说，折叠态不该占这个宽度。
 */
private fun subtitleOf(ch: ChannelRow): String {
    val parts = mutableListOf<String>()
    if (ch.vendor.isNotBlank()) parts += ch.vendor
    val q = ch.quota
    when {
        q.detail.isNotBlank() -> parts += q.detail
        q.expiryMs > 0 -> parts += Fmt.remaining(q.expiryMs)
        q.never -> parts += "永久额度"
        else -> Unit
    }
    // 什么都没有就返回空串：折叠行只留名字 + 状态药丸，比多一行「—」干净。
    return parts.joinToString(" · ").ifBlank { ch.note }
}
