package com.lampguide.lampnative.ui.screens

import androidx.compose.foundation.background
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
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.Mappers
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.ui.Card
import com.lampguide.lampnative.ui.EmptyBlock
import com.lampguide.lampnative.ui.ErrorBlock
import com.lampguide.lampnative.ui.GhostButton
import com.lampguide.lampnative.ui.KeyValue
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.LoadingRow
import com.lampguide.lampnative.ui.Pill
import com.lampguide.lampnative.ui.ScreenScaffold
import com.lampguide.lampnative.ui.SubtleButton
import com.lampguide.lampnative.ui.TableCard
import com.lampguide.lampnative.ui.TableDivider
import com.lampguide.lampnative.ui.WarnBar
import com.lampguide.lampnative.ui.theme.Fmt
import com.lampguide.lampnative.ui.theme.Lamp

/**
 * S1 状态（首屏，单滚动页）。
 *
 * 目的只有一个：三秒回答「在跑吗 / 能用吗 / 花了多少」。
 * 数据来源：`state` 一次拉全。
 */
@Composable
fun S1Status(
    load: Load<StateView>,
    onStartGateway: () -> Unit,
    onGoChannels: () -> Unit,
    onGoConnect: () -> Unit,
    onGoModels: () -> Unit,
    onCopy: (label: String, text: String) -> Unit,
) {
    ScreenScaffold(title = "Lamp Guide", subtitle = "本机网关，给手机上的 AI 客户端用") {
        when (load) {
            Load.Loading -> LoadingRow("正在读取网关状态…")
            is Load.Error -> ErrorBlock(load.err, onStart = onStartGateway)
            is Load.Empty -> EmptyBlock("网关没有返回任何状态")
            is Load.Data -> S1Body(
                s = load.value,
                onGoChannels = onGoChannels,
                onGoConnect = onGoConnect,
                onGoModels = onGoModels,
                onCopy = onCopy,
            )
        }
    }
}

@Composable
private fun S1Body(
    s: StateView,
    onGoChannels: () -> Unit,
    onGoConnect: () -> Unit,
    onGoModels: () -> Unit,
    onCopy: (String, String) -> Unit,
) {
    val today = Mappers.today(s.stats, s.now.take(10).ifBlank { "" })
    val available = Mappers.availableCount(s.models)
    val unprobed = Mappers.unprobedCount(s.models)
    val probedOut = Mappers.allProbed(s.models)
    val dshGo = s.agents.firstOrNull { it.id == "dsh-go" }

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(bottom = 88.dp),
    ) {
        // ── 顶部大卡：服务状态 ──────────────────────────────────────────
        Column(Modifier.padding(horizontal = 20.dp)) {
            Card(emphasized = true) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            "网关在跑",
                            color = Lamp.colors.text,
                            style = MaterialTheme.typography.headlineSmall,
                        )
                        Text(
                            "端口 ${s.port} · 已运行 ${Fmt.uptime(s.uptimeSec)}",
                            color = Lamp.colors.textDim,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                    Pill("运行中", Lamp.colors.ok)
                }
                Spacer(Modifier.height(14.dp))
                KeyValue("版本", if (s.version.isBlank()) "—" else s.version, mono = true)
                KeyValue("启动于", s.startedAt.ifBlank { "—" }, mono = true)
            }
        }

        // ── 中段：今日 token / 今日请求数，一张卡里的两列 ──────────────────
        // 两个各自带描边的卡并排时，中间那条缝会把「今天」说成两件事；它们是同一个
        // 计数的两个量，靠一条内部分隔线分开就够。数字用同一档字号，眼睛能横着比。
        Column(Modifier.padding(horizontal = 20.dp, vertical = 12.dp)) {
            TableCard {
                androidx.compose.foundation.layout.Row(
                    Modifier.fillMaxWidth(),
                    verticalAlignment = androidx.compose.ui.Alignment.Top,
                ) {
                    com.lampguide.lampnative.ui.BigNumber(
                        Fmt.tokens(today.output),
                        "今日输出 token",
                        modifier = Modifier.weight(1f),
                    )
                    Column(
                        Modifier
                            .padding(horizontal = 16.dp)
                            .width(1.dp)
                            .height(56.dp)
                            .background(Lamp.colors.line),
                    ) {}
                    com.lampguide.lampnative.ui.BigNumber(
                        Fmt.tokens(today.requests),
                        "今日请求数",
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }

        // ── 接入卡「标签—值—复制」三列一张表 ────────────────────────────
        // 把复制按钮从卡片底部那一排挪进每一行：哪个按钮复制哪一行，靠位置说清；
        // 底下并排两个"复制 Base URL / 复制主 Key"时，用户要先读标签再回头找按钮。
        Column(Modifier.padding(horizontal = 20.dp)) {
            TableCard(title = "交给客户端的东西") {
                EndpointRow("Base URL", s.baseURL.ifBlank { "—" }) {
                    onCopy("Base URL", s.baseURL)
                }
                TableDivider()
                EndpointRow("主 Key", Mappers.maskKey(s.mainKey)) {
                    onCopy("主 Key", s.mainKey)
                }
                if (s.settings.allowLan && s.lanBaseURL.isNotBlank()) {
                    TableDivider()
                    EndpointRow("局域网", s.lanBaseURL) { onCopy("局域网地址", s.lanBaseURL) }
                }
            }
        }

        // ── 可用模型数 ─────────────────────────────────────────────────
        Column(Modifier.padding(horizontal = 20.dp, vertical = 12.dp)) {
            Card(onClick = onGoModels) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("可用模型", color = Lamp.colors.text, style = MaterialTheme.typography.titleMedium)
                        Text(
                            "现在能用 $available / 共 ${s.models.size}" +
                                if (unprobed > 0) " · $unprobed 个还没探测" else "",
                            color = Lamp.colors.textDim,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                    Text("›", color = Lamp.colors.textFaint, style = MaterialTheme.typography.headlineSmall)
                }
            }
        }

        // ── §6.4 兜底黄条：已开但签不出专属 Key ─────────────────────────
        if (dshGo != null && dshGo.enabled && s.agentKeys["dsh-go"].isNullOrBlank()) {
            WarnBar("DSH Go 的接入开关是开的，但没有签发专属 Key（网关可能跑在只读模式下）。请在接入页关掉再打开一次。")
        }

        // ── 底部：还没接进 DSH Go ───────────────────────────────────────
        if (dshGo != null && !dshGo.enabled) {
            Column(Modifier.padding(horizontal = 20.dp)) {
                Card(onClick = onGoConnect) {
                    Row(Modifier.fillMaxWidth(), verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text("还没接进 DSH Go", color = Lamp.colors.text, style = MaterialTheme.typography.titleMedium)
                            Text(
                                if (dshGo.installed) "已安装，打开开关就会签发专属 Key" else "未检测到 DSH Go",
                                color = Lamp.colors.textDim,
                                style = MaterialTheme.typography.bodySmall,
                            )
                        }
                        Text("›", color = Lamp.colors.textFaint, style = MaterialTheme.typography.headlineSmall)
                    }
                }
            }
        }

        // 「一个都不可用」只在真的判过之后才说：网关刚起来的那几分钟整表都是
        // 未探测，这时候抬这句话是错的（用户会去渠道页找一个并不存在的问题）。
        if (available == 0 && s.models.isNotEmpty() && probedOut) {
            WarnBar("一个模型都不可用。去渠道页看看账号是不是掉了。")
            Row(Modifier.padding(horizontal = 20.dp)) {
                GhostButton("去渠道页") { onGoChannels() }
            }
        }
        Spacer(Modifier.width(0.dp))
    }
}

/**
 * 一行"标签 — 地址/Key — 复制"。
 *
 * 复制按钮跟着行走到右边，不再把两个复制按钮并排放在卡片底下：一行里的地址
 * 和它的按钮是同一件事，离开那一行就要靠用户自己把两处对起来。
 */
@Composable
private fun EndpointRow(label: String, value: String, onCopy: () -> Unit) {
    androidx.compose.foundation.layout.Row(
        Modifier.fillMaxWidth().padding(vertical = 8.dp),
        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
    ) {
        androidx.compose.material3.Text(
            label,
            color = Lamp.colors.textDim,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.width(76.dp),
        )
        androidx.compose.material3.Text(
            value,
            color = Lamp.colors.text,
            style = MaterialTheme.typography.bodySmall.copy(
                fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
            ),
            maxLines = 1,
            overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Spacer(Modifier.width(10.dp))
        SubtleButton("复制", onClick = onCopy)
    }
}
