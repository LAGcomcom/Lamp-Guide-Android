package com.lampguide.lampnative.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
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
import com.lampguide.lampnative.ui.ScreenScaffold
import com.lampguide.lampnative.ui.TableCard
import com.lampguide.lampnative.ui.TableDivider
import com.lampguide.lampnative.ui.WarnBar
import com.lampguide.lampnative.ui.theme.Lamp

/**
 * S3 接入（DSH Go）。目的：把钥匙交到另一个 App 手里，一次就成。
 *
 * 数据来源：`state.agents`（id=dsh-go 那一项）+ `state.agentKeys["dsh-go"]`。
 * 写：`agent/dsh-go/enable|disable`。
 */
@Composable
fun S3Connect(
    load: Load<StateView>,
    keyDrifted: Boolean,
    onToggle: (Boolean) -> Unit,
    onOpenDshGo: () -> Unit,
    onCopy: (String, String) -> Unit,
) {
    ScreenScaffold(title = "接入 DSH Go", subtitle = "把网关的钥匙交给同一个手机上的那个 App") {
        when (load) {
            Load.Loading -> LoadingRow()
            is Load.Error -> ErrorBlock(load.err)
            is Load.Empty -> EmptyBlock("网关没有返回状态")
            is Load.Data -> S3Body(load.value, keyDrifted, onToggle, onOpenDshGo, onCopy)
        }
    }
}

@Composable
private fun S3Body(
    s: StateView,
    keyDrifted: Boolean,
    onToggle: (Boolean) -> Unit,
    onOpenDshGo: () -> Unit,
    onCopy: (String, String) -> Unit,
) {
    val agent = s.agents.firstOrNull { it.id == "dsh-go" }
    if (agent == null) {
        EmptyBlock("网关没有报出 dsh-go 这个接入项", "这台设备的网关构建可能不含安卓接入适配器")
        return
    }
    val key = s.agentKeys["dsh-go"].orEmpty()
    val available = Mappers.availableCount(s.models)

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 12.dp)
            .padding(bottom = 88.dp),
    ) {
        Card(emphasized = true) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("DSH Go", color = Lamp.colors.text, style = MaterialTheme.typography.headlineSmall)
                    Text(
                        when {
                            agent.installed && agent.version.isNotBlank() -> "已安装 · 版本 ${agent.version}"
                            agent.installed -> "已安装"
                            else -> "未检测到 DSH Go"
                        },
                        color = Lamp.colors.textDim,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                // 全应用一个开关形状：40×20 细轨道（LampSwitch），
                // 别处用 Material 的 52×32 就比这一屏所有的东西都重一档。
                com.lampguide.lampnative.ui.LampSwitch(checked = agent.enabled, onChange = onToggle)
            }
            if (agent.warn.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(agent.warn, color = Lamp.colors.warn, style = MaterialTheme.typography.bodySmall)
            }
        }

        // §6.4：已开但签不出专属 Key —— 这是「静默假成功」那一处的兜底。
        if (agent.enabled && key.isBlank()) {
            WarnBar("开关是开的，但网关没有签发 dsh-go 专属 Key。这通常是网关跑在只读模式下的表现；关掉再打开一次，仍不行说明这一侧签不出 Key。")
        }
        // §6.8：Key 变了要说出来（本地记的是 hash，不是 Key 本身）。
        if (agent.enabled && key.isNotBlank() && keyDrifted) {
            WarnBar("DSH Go 里存的旧 Key 已失效，请重新粘贴。重装这个 APK 会重建配置，主 Key 与专属 Key 都会变。")
        }

        Spacer(Modifier.height(12.dp))

        if (!agent.enabled) {
            Card {
                Text("打开开关会发生什么", color = Lamp.colors.text, style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.height(8.dp))
                KeyValue("Base URL", s.baseURL.ifBlank { "—" }, mono = true)
                KeyValue("可用模型", "$available / ${s.models.size}")
                KeyValue("专属 Key", "打开后单独签发一把，与主 Key 不同")
            }
        } else {
            // 三步是一组同构的行：序号 / 做什么 / 那一样东西 / 动作。写成一张表，
            // 三行的按钮才会落在同一条竖线上；原来用 Spacer 串起来，按钮跟着
            // 上一行的文字高度漂，三步看起来像三件不相干的事。
            TableCard(title = "三步接进去") {
                Step(1, "复制 Base URL", s.baseURL.ifBlank { "—" }, "复制") { onCopy("Base URL", s.baseURL) }
                TableDivider()
                Step(2, "复制专属 Key", Mappers.maskKey(key), "复制") { onCopy("专属 Key", key) }
                TableDivider()
                Step(
                    3,
                    "打开 DSH Go 的模型页",
                    if (agent.installed) "贴进「设置 › 模型 › 添加渠道」" else "未检测到 DSH Go，装好再回来",
                    "打开",
                ) { onOpenDshGo() }
            }
        }

        Spacer(Modifier.height(12.dp))
        Card {
            Text("这一页为什么不能全自动", color = Lamp.colors.textDim, style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(6.dp))
            Text(
                // 兜底文案要说清"卡在哪一步"，不是重复一句本页标题。
                // 安卓沙箱：别的 App 的私有目录读不到，也写不进。
                agent.detail.ifBlank {
                    "DSH Go 的接口配置存在它自己的私有目录里，安卓沙箱不允许本应用写另一个 App 的数据。" +
                        "所以这三步只能由你贴一次；贴完之后网关那边认的是专属 Key，之后不用再动。"
                },
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}

@Composable
private fun Step(n: Int, title: String, value: String, action: String, onAct: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StepDot(n)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(title, color = Lamp.colors.text, style = MaterialTheme.typography.bodyMedium)
            Text(
                value,
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.labelSmall.copy(
                    fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                ),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.width(10.dp))
        GhostButton(action, onClick = onAct)
    }
}

@Composable
private fun StepDot(n: Int) {
    Box(
        Modifier
            .size(22.dp)
            .clip(CircleShape)
            .background(Lamp.colors.amberSoft),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            "$n",
            color = Lamp.colors.amber,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = FontWeight.SemiBold,
        )
    }
}
