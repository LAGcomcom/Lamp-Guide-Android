package com.lampguide.lampnative.ui.screens

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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.ui.Card
import com.lampguide.lampnative.ui.EmptyBlock
import com.lampguide.lampnative.ui.ErrorBlock
import com.lampguide.lampnative.ui.GhostButton
import com.lampguide.lampnative.ui.KeyValue
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.ButtonTone
import com.lampguide.lampnative.ui.LampButton
import com.lampguide.lampnative.ui.LoadingRow
import com.lampguide.lampnative.ui.Segmented
import com.lampguide.lampnative.ui.WarnBar
import com.lampguide.lampnative.ui.theme.Lamp
import kotlinx.serialization.json.JsonPrimitive

/**
 * S6 设置。只放手机上真的会动的项。
 *
 * **刻意不做**：一键更新、托盘、开机自启切换、公告横幅、导出 CSV、usage.csv、
 * 订阅轮换、OpenCode 面板。日志面只保留「最近 20 条错误」，且固定
 * `level=error&limit=20`。
 */
@Composable
fun S6Settings(
    load: Load<StateView>,
    gatewayVersion: String,
    errors: Load<List<String>>,
    onSave: (String, JsonPrimitive, String) -> Unit,
    onRotateKey: () -> Unit,
    onRestartService: () -> Unit,
    onBatteryExempt: () -> Unit,
    onCopyDataDir: () -> Unit,
    onLoadErrors: () -> Unit,
) {
    when (load) {
        Load.Loading -> LoadingRow()
        is Load.Error -> ErrorBlock(load.err)
        is Load.Empty -> EmptyBlock("网关没有返回设置")
        is Load.Data -> S6Body(
            load.value, gatewayVersion, errors, onSave, onRotateKey,
            onRestartService, onBatteryExempt, onCopyDataDir, onLoadErrors,
        )
    }
}

@Composable
private fun S6Body(
    s: StateView,
    gatewayVersion: String,
    errors: Load<List<String>>,
    onSave: (String, JsonPrimitive, String) -> Unit,
    onRotateKey: () -> Unit,
    onRestartService: () -> Unit,
    onBatteryExempt: () -> Unit,
    onCopyDataDir: () -> Unit,
    onLoadErrors: () -> Unit,
) {
    var port by remember { mutableStateOf(s.port.toString()) }
    var maxTokens by remember { mutableStateOf(s.settings.defaultMaxTokens.toString()) }
    var probeMinutes by remember { mutableStateOf(s.settings.probeIntervalMinutes.toString()) }
    var proxyUrl by remember { mutableStateOf(s.settings.proxyURL) }
    var confirmRotate by remember { mutableStateOf(false) }

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(vertical = 8.dp)
            .padding(bottom = 88.dp),
    ) {
        // ── 网络 ────────────────────────────────────────────────────────
        Group("网络") {
            NumberField("端口", port, "8787") { port = it }
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                GhostButton("保存端口", onClick = {
                    val n = port.toIntOrNull()
                    if (n == null || n !in 1..65535) {
                        onSave("__error__", JsonPrimitive(port), "端口必须是 1–65535 的整数")
                    } else {
                        onSave("port", JsonPrimitive(n), "端口已保存，需要重启网关才生效")
                    }
                })
                GhostButton("重启网关") { onRestartService() }
            }
            Spacer(Modifier.height(6.dp))
            Text(
                "改端口会让 DSH Go 里存的地址失效：那边存的是 ${s.port}。",
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.labelSmall,
            )

            Spacer(Modifier.height(12.dp))
            ToggleRow(
                "允许局域网访问",
                if (s.settings.allowLan) "已开启 · ${s.lanBaseURL.ifBlank { "—" }}" else "只允许本机",
                s.settings.allowLan,
            ) { onSave("allowLan", JsonPrimitive(it), if (it) "已允许局域网访问" else "已关闭局域网访问") }

            Spacer(Modifier.height(12.dp))
            Text(
                "代理模式",
                color = Lamp.colors.textDim,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(bottom = 6.dp),
            )
            Segmented(
                options = listOf("direct" to "直连", "system" to "系统代理", "custom" to "自定义"),
                selected = s.settings.proxyMode,
                onPick = { onSave("proxyMode", JsonPrimitive(it), "代理模式已改为$it") },
            )
            if (s.settings.proxyMode == "custom") {
                Spacer(Modifier.height(8.dp))
                com.lampguide.lampnative.ui.LampField(
                    value = proxyUrl,
                    onValueChange = { proxyUrl = it },
                    label = "代理地址",
                    placeholder = "http://127.0.0.1:7890",
                )
                Spacer(Modifier.height(6.dp))
                GhostButton("保存代理地址") {
                    onSave("proxyUrl", JsonPrimitive(proxyUrl), "代理地址已保存")
                }
            }
        }

        // ── 生成 ────────────────────────────────────────────────────────
        Group("生成") {
            NumberField("默认 max tokens", maxTokens, "默认值") { maxTokens = it }
            Spacer(Modifier.height(8.dp))
            GhostButton("保存") {
                val n = maxTokens.toIntOrNull()
                if (n == null || n <= 0) {
                    onSave("__error__", JsonPrimitive(maxTokens), "max tokens 必须是正整数")
                } else {
                    onSave("defaultMaxTokens", JsonPrimitive(n), "默认 max tokens 已保存")
                }
            }
            Spacer(Modifier.height(12.dp))
            Text("默认 effort", color = Lamp.colors.textDim, style = MaterialTheme.typography.bodySmall)
            Segmented(
                options = listOf("low" to "low", "medium" to "medium", "high" to "high"),
                selected = s.settings.defaultEffort,
                onPick = { onSave("defaultEffort", JsonPrimitive(it), "默认 effort 已改为 $it") },
            )
            Spacer(Modifier.height(12.dp))
            NumberField("探测间隔（分钟）", probeMinutes, "分钟") { probeMinutes = it }
            Spacer(Modifier.height(8.dp))
            GhostButton("保存") {
                val n = probeMinutes.toIntOrNull()
                if (n == null || n <= 0) {
                    onSave("__error__", JsonPrimitive(probeMinutes), "探测间隔必须是正整数")
                } else {
                    onSave("probeIntervalMinutes", JsonPrimitive(n), "探测间隔已保存")
                }
            }
        }

        // ── 后台 ────────────────────────────────────────────────────────
        Group("后台常驻") {
            ToggleRow(
                "允许后台常驻",
                "小米/HyperOS 会掐后台；开启后系统不再限制本应用",
                true,
            ) { onBatteryExempt() }
            Spacer(Modifier.height(8.dp))
            Text(
                "安卓 15+ 的前台服务有 6 小时上限，到点通知会变成「需要重新打开」。这是系统限制，不是网关故障。",
                color = Lamp.colors.textFaint,
                style = MaterialTheme.typography.labelSmall,
            )
        }

        // ── 密钥 ────────────────────────────────────────────────────────
        Group("密钥") {
            KeyValue("主 Key", com.lampguide.lampnative.data.Mappers.maskKey(s.mainKey), mono = true)
            val dshKey = s.agentKeys["dsh-go"].orEmpty()
            KeyValue("DSH Go Key", if (dshKey.isBlank()) "未签发" else com.lampguide.lampnative.data.Mappers.maskKey(dshKey), mono = true)
            Spacer(Modifier.height(10.dp))
            if (!confirmRotate) {
                GhostButton("重新生成主 Key") { confirmRotate = true }
            } else {
                WarnBar("重新生成会让所有已接入的客户端立刻失效（包括 DSH Go）。")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    // 破坏性动作走 Critical：它不该长得和"保存端口"这类正向主按钮一样。
                    LampButton(
                        text = "确认重新生成",
                        onClick = { confirmRotate = false; onRotateKey() },
                        tone = ButtonTone.Critical,
                    )
                    GhostButton("取消") { confirmRotate = false }
                }
            }
        }

        // ── 日志（只保留最近 20 条错误） ──────────────────────────────────
        Group("最近的错误") {
            when (errors) {
                Load.Loading -> LoadingRow("读取中…")
                is Load.Error -> ErrorBlock(errors.err)
                is Load.Empty -> Text("最近没有错误", color = Lamp.colors.textFaint, style = MaterialTheme.typography.bodySmall)
                is Load.Data -> for (line in errors.value) {
                    Text(line, color = Lamp.colors.textDim, style = MaterialTheme.typography.labelSmall)
                }
            }
            Spacer(Modifier.height(8.dp))
            GhostButton("读取最近 20 条错误") { onLoadErrors() }
        }

        // ── 数据目录 ──────────────────────────────────────────────────────
        Group("数据目录") {
            KeyValue("位置", s.dataDir.ifBlank { "—" }, mono = true)
            Spacer(Modifier.height(8.dp))
            GhostButton("复制路径") { onCopyDataDir() }
        }

        // ── 关于 ──────────────────────────────────────────────────────────
        Group("关于") {
            KeyValue("应用版本", "1.0.0")
            KeyValue("网关版本", if (s.version.isBlank()) "—" else s.version, mono = true)
            KeyValue("gateway.Version", if (gatewayVersion.isBlank()) "—" else gatewayVersion, mono = true)
            KeyValue("构建", "arm64-v8a · targetSdk 36")
        }
    }
}

@Composable
private fun Group(title: String, content: @Composable () -> Unit) {
    Column(Modifier.padding(horizontal = 20.dp, vertical = 6.dp)) {
        Text(
            title,
            color = Lamp.colors.textDim,
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(bottom = 6.dp),
        )
        Card { content() }
    }
}

@Composable
private fun NumberField(label: String, value: String, placeholder: String, onChange: (String) -> Unit) {
    com.lampguide.lampnative.ui.LampField(
        value = value,
        onValueChange = { onChange(it.filter { c -> c.isDigit() }) },
        label = label,
        placeholder = placeholder,
        numeric = true,
    )
}

@Composable
private fun ToggleRow(title: String, subtitle: String, checked: Boolean, onChecked: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, color = Lamp.colors.text, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(1.dp))
            Text(subtitle, color = Lamp.colors.textFaint, style = MaterialTheme.typography.labelSmall)
        }
        Spacer(Modifier.width(12.dp))
        com.lampguide.lampnative.ui.LampSwitch(checked = checked, onChange = onChecked)
    }
}

