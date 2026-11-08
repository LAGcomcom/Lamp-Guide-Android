package com.lampguide.lampnative.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.BarChart
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Hub
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.lampguide.lampnative.ui.screens.S1Status
import com.lampguide.lampnative.ui.screens.S2Channels
import com.lampguide.lampnative.ui.screens.S3Connect
import com.lampguide.lampnative.ui.screens.S4Models
import com.lampguide.lampnative.ui.screens.S5Usage
import com.lampguide.lampnative.ui.screens.S6Settings
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.Radius

/**
 * 六屏的导航：**悬浮胶囊**，不是贴底通栏。
 *
 * 为什么悬浮：桌面版的内容是浮在 #F3F3F3 地面上的一张张半透明白卡，通栏底栏
 * 会在屏幕底部切出一条永久的实色带，把「卡浮在地面」这件事说反。改成一块
 * 离底 16dp、左右收 16dp 的胶囊后，卡片可以滚到它下面去，层次才和桌面版一致。
 *
 * 胶囊里放四屏，接入/设置两枚放在胶囊右侧的独立圆钮里——它们是「出门」的动作，
 * 不属于六屏切换。省掉一整条底栏后，还给内容留出了 56dp 的纵向空间。
 */
enum class Tab(val label: String, val icon: ImageVector) {
    Status("状态", Icons.Outlined.Lightbulb),
    Channels("渠道", Icons.Outlined.Hub),
    Models("模型", Icons.Outlined.Extension),
    Usage("用量", Icons.Outlined.BarChart),
}

enum class Overlay { Connect, Settings }

@Composable
fun LampApp(vm: AppViewModel, onStartGateway: () -> Unit, actions: AppActions) {
    // collectAsStateWithLifecycle：切后台时停掉收集，回前台立刻拿到最新一帧。
    // 用 collectAsState 会在进程被 START_STICKY 重建后停在旧值上。
    val state by vm.state.collectAsStateWithLifecycle()
    val channels by vm.channels.collectAsStateWithLifecycle()
    val checkin by vm.checkin.collectAsStateWithLifecycle()
    val agg by vm.aggregate.collectAsStateWithLifecycle()
    val toast by vm.toast.collectAsStateWithLifecycle()
    val loginFor by vm.loginChannel.collectAsStateWithLifecycle()

    var tab by remember { mutableStateOf(Tab.Status) }
    var overlay by remember { mutableStateOf<Overlay?>(null) }
    val snackbar = remember { SnackbarHostState() }

    LaunchedEffect(toast) {
        toast?.let {
            snackbar.showSnackbar(it)
            vm.consumeToast()
        }
    }

    // 首帧拉数据。网关由前台服务起，起完要几百毫秒，所以这里不假设它已经就绪：
    // 立刻拉一次，随后在有错或还没数据时继续重试（见 AppViewModel.awaitGateway）。
    LaunchedEffect(Unit) { vm.bootstrap() }

    Box(Modifier.fillMaxSize().background(Lamp.colors.ground)) {
        Scaffold(
            containerColor = Lamp.colors.ground,
            contentWindowInsets = androidx.compose.foundation.layout.WindowInsets(0, 0, 0, 0),
            snackbarHost = {
                Box(Modifier.navigationBarsPadding().padding(bottom = 84.dp)) {
                    SnackbarHost(snackbar)
                }
            },
            bottomBar = {},
        ) { _ ->
            Column(Modifier.fillMaxSize().statusBarsPadding()) {
                // 覆盖页必须有出口：没有返回的话「设置」就是一条单向门。
                if (overlay != null) {
                    OverlayBack(label = if (overlay == Overlay.Connect) "接入" else "设置") {
                        overlay = null
                    }
                }
                Box(Modifier.weight(1f)) {
                    when (overlay) {
                        Overlay.Connect -> S3Connect(
                            load = state,
                            keyDrifted = actions.keyDrifted(),
                            onToggle = { vm.setAgentEnabled("dsh-go", it) },
                            onOpenDshGo = { actions.openDshGo() },
                            onCopy = { label, text -> vm.notify(actions.copy(label, text)) },
                        )
                        Overlay.Settings -> S6Settings(
                            load = state,
                            gatewayVersion = vm.gatewayVersion(),
                            errors = actions.errors,
                            onSave = { field, value, note ->
                                if (field == "__error__") vm.notify(note)
                                else vm.saveSettings(mapOf(field to value), note)
                            },
                            onRotateKey = { vm.rotateKey() },
                            onRestartService = { actions.restartGateway(); vm.notify("网关已重启") },
                            onBatteryExempt = { actions.requestBatteryExemption() },
                            onCopyDataDir = {
                                val dir = (state as? Load.Data)?.value?.dataDir.orEmpty()
                                vm.notify(actions.copy("数据目录", dir))
                            },
                            onLoadErrors = { actions.loadErrors() },
                        )
                        null -> when (tab) {
                            Tab.Status -> S1Status(
                                load = state,
                                onStartGateway = onStartGateway,
                                onGoChannels = { tab = Tab.Channels },
                                onGoConnect = { overlay = Overlay.Connect },
                                onGoModels = { tab = Tab.Models },
                                onCopy = { label, text -> vm.notify(actions.copy(label, text)) },
                            )
                            Tab.Channels -> S2Channels(
                                channels = channels,
                                checkin = checkin,
                                onRunCheckin = { vm.runCheckin() },
                                onAddAccount = { vm.openLogin(it) },
                                onLogout = { id, key -> vm.logout(id, key) },
                            )
                            Tab.Models -> S4Models(
                                load = state,
                                agg = agg,
                                onProbe = { id, channel -> vm.probe(id, channel) },
                                onHide = { id, hidden -> vm.setModelHidden(id, hidden) },
                                onProbePending = { vm.probePending(it) },
                            )
                            Tab.Usage -> S5Usage(load = state)
                        }
                    }
                    if (overlay == null && loginFor != null) {
                        LoginSheet(
                            channelId = loginFor!!,
                            vm = vm,
                            onOpenUrl = { url -> vm.notify(actions.openUrl(url)) },
                            onCopy = { label, text -> vm.notify(actions.copy(label, text)) },
                            onClose = { vm.closeLogin() },
                        )
                    }
                }
                if (overlay == null) {
                    FloatingNav(
                        current = tab,
                        onPick = { tab = it },
                        onOverlay = { overlay = it },
                    )
                }
            }
        }
    }
}

/**
 * 悬浮胶囊导航。
 *
 * 三件事是它的全部：离底 16dp（不贴边）、圆角 28（比卡片大一档，一眼看出是另一层）、
 * 一块比地面亮一档的实底 + 细描边（它浮在卡片之上，不能是半透明的）。
 * 选中项用一块强调色底 + 白字，是这一屏唯一允许的实色块。
 */
@Composable
private fun FloatingNav(current: Tab, onPick: (Tab) -> Unit, onOverlay: (Overlay) -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(start = 16.dp, end = 16.dp, bottom = 16.dp, top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(
            Modifier
                .weight(1f)
                .clip(RoundedCornerShape(28.dp))
                .background(Lamp.colors.surfaceSolid)
                .border(1.dp, Lamp.colors.lineStrong, RoundedCornerShape(28.dp))
                .padding(horizontal = 6.dp, vertical = 6.dp),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            for (t in Tab.entries) {
                NavPill(t, t == current) { onPick(t) }
            }
        }
        NavButton(Icons.Outlined.Settings) { onOverlay(Overlay.Settings) }
    }
}

@Composable
private fun RowScope.NavPill(t: Tab, on: Boolean, onClick: () -> Unit) {
    val bg by animateFloatAsState(if (on) 1f else 0f, label = "pill")
    val source = remember { MutableInteractionSource() }
    Column(
        Modifier
            .weight(1f)
            .clip(RoundedCornerShape(24.dp))
            .background(lerpColor(Color.Transparent, Lamp.colors.amber, bg))
            .clickable(interactionSource = source, indication = null, onClick = onClick)
            .padding(vertical = 8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Icon(
            t.icon,
            contentDescription = null,
            tint = if (on) Lamp.colors.onAmber else Lamp.colors.textDim,
            modifier = Modifier.size(19.dp),
        )
        Text(
            t.label,
            color = if (on) Lamp.colors.onAmber else Lamp.colors.textDim,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

@Composable
private fun NavButton(icon: ImageVector, onClick: () -> Unit) {
    Box(
        Modifier
            .size(56.dp)
            .clip(RoundedCornerShape(28.dp))
            .background(Lamp.colors.surfaceSolid)
            .border(1.dp, Lamp.colors.lineStrong, RoundedCornerShape(28.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = "设置", tint = Lamp.colors.text, modifier = Modifier.size(22.dp))
    }
}

private fun lerpColor(a: Color, b: Color, t: Float): Color = Color(
    red = a.red + (b.red - a.red) * t,
    green = a.green + (b.green - a.green) * t,
    blue = a.blue + (b.blue - a.blue) * t,
    alpha = a.alpha + (b.alpha - a.alpha) * t,
)

@Composable
private fun OverlayBack(label: String, onBack: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 20.dp, top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .clip(RoundedCornerShape(Radius.CONTROL.dp))
                .background(Lamp.colors.surfaceHigh)
                .border(1.dp, Lamp.colors.line, RoundedCornerShape(Radius.CONTROL.dp))
                .clickable(onClick = onBack)
                .padding(horizontal = 12.dp, vertical = 7.dp),
        ) {
            Text("‹ 返回", color = Lamp.colors.text, style = MaterialTheme.typography.bodySmall)
        }
        Spacer(Modifier.width(10.dp))
        Text(label, color = Lamp.colors.textDim, style = MaterialTheme.typography.titleMedium)
    }
}

/** 界面之外的那几件只有 Activity 能做（SAF、跳转、剪贴板、电池豁免）。 */
interface AppActions {
    fun copy(label: String, text: String): String
    /** 打开一个 http(s) 授权页；返回一句可上屏的结果（"已在浏览器打开" / 失败原因）。 */
    fun openUrl(url: String): String
    fun openDshGo()
    fun restartGateway()
    fun requestBatteryExemption()
    fun loadErrors()
    fun keyDrifted(): Boolean
    val errors: Load<List<String>>
}
