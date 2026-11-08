package com.lampguide.lampnative.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.lampguide.lampnative.data.AdminClient
import com.lampguide.lampnative.data.ChannelRow
import com.lampguide.lampnative.data.LoginResponse
import com.lampguide.lampnative.data.LoginSession
import com.lampguide.lampnative.data.PollResult
import com.lampguide.lampnative.ui.theme.Fmt
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.Radius
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * 添加账号的居中模态。两条路，按渠道自己报的 login.kind 决定：
 *
 * 1. 有授权入口（device / pkce / poll / qr / cliflow / ticket / claw / qoder-device）
 *    → channel/login 拿会话（openUrl / userCode / qrImage），再 channel/poll?id=
 *    按会话给的节奏轮询，成功后账号自动出现在清单里 —— 与桌面端同一套流程。
 * 2. 只有贴 Key（paste）→ channel/token。有授权入口的渠道也把贴 Key 收在
 *    「也可以直接贴 Key」后面，因为抄 session 有时比走一遍授权更快。
 *
 * 上一版是贴在底部的 Column + surfaceHigh 底（深色下只有 8% 白），真机上
 * 几乎看不见，也认不出这是一个弹窗。这里改成真正的 Dialog：模态、遮罩、
 * 实心底卡片、居中，内容超高时自己滚。
 */
private val KIND_STEP = mapOf(
    "device" to "1. 点「打开授权页面」；2. 把下面的码填进去；3. 回来等这里变绿。",
    "pkce" to "1. 点「打开授权页面」；2. 在浏览器里登录并同意；3. 回来等这里变绿。",
    "poll" to "1. 点「打开授权页面」；2. 在浏览器里登录并同意；3. 回来等这里变绿。",
    "cliflow" to "1. 点「打开授权页面」；2. 在浏览器里登录并同意；3. 回来等这里变绿。",
    "ticket" to "1. 点「打开授权页面」；2. 在浏览器里登录；3. 回来后这里自动换回凭据，等它变绿。",
    "claw" to "1. 点「打开授权页面」；2. 在浏览器里登录并同意；3. 回来等这里变绿。",
    "qoder-device" to "1. 点「打开授权页面」；2. 在浏览器里登录 Qoder 并确认；3. 回来等这里变绿。",
    "qr" to "1. 用这一家的手机客户端扫下面的码；2. 手机上确认；3. 回来等这里变绿。",
)

@Composable
fun LoginSheet(
    channelId: String,
    vm: AppViewModel,
    onOpenUrl: (String) -> Unit,
    onCopy: (String, String) -> Unit,
    onClose: () -> Unit,
) {
    val channelsLoad by vm.channels.collectAsState()
    val channel: ChannelRow? = (channelsLoad as? Load.Data)?.value?.channels?.firstOrNull { it.id == channelId }
    val client = remember { AdminClient() }
    val scope = rememberCoroutineScope()

    val loginSpec = channel?.login
    val kind = loginSpec?.kind.orEmpty()
    val hasWebLogin = kind.isNotBlank() && kind != "paste"

    var session by remember { mutableStateOf<LoginSession?>(null) }
    var starting by remember { mutableStateOf(false) }
    var polling by remember { mutableStateOf(false) }
    var statusText by remember { mutableStateOf("") }
    var errorText by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var showPaste by remember { mutableStateOf(!hasWebLogin) }
    var pollJob by remember { mutableStateOf<kotlinx.coroutines.Job?>(null) }

    fun stopPolling() {
        polling = false
        pollJob?.cancel()
        pollJob = null
    }

    suspend fun pollLoop(s: LoginSession) {
        polling = true
        val deadline = s.expiresAtMs.takeIf { it > 0 } ?: (System.currentTimeMillis() + 600_000L)
        val interval = s.intervalMs.takeIf { it > 0 }?.coerceAtLeast(1000L) ?: 1500L
        while (polling) {
            delay(interval)
            if (!polling) break
            if (System.currentTimeMillis() > deadline) {
                polling = false
                statusText = "登录已超时，点「重新开始」再来一次。"
                break
            }
            when (val r = client.get<PollResult>(AdminClient.Admin.poll(channelId))) {
                is AdminClient.Result.Ok -> {
                    if (r.value.state == "ok") {
                        polling = false
                        statusText = "已登录，账号已写入这一家。"
                        vm.notify("账号已添加")
                        vm.loadChannels()
                        vm.loadCheckin()
                        vm.loadState(true)
                        onClose()
                        break
                    }
                }
                is AdminClient.Result.Err -> {
                    polling = false
                    errorText = r.message
                    break
                }
            }
        }
    }

    fun start() {
        if (starting) return
        starting = true
        errorText = ""
        statusText = ""
        scope.launch {
            val body = JsonObject(mapOf("id" to JsonPrimitive(channelId)))
            when (val r = client.get<LoginResponse>(AdminClient.Admin.CHANNEL_LOGIN, body)) {
                is AdminClient.Result.Ok -> {
                    val s = r.value.session
                    session = s
                    statusText = "正在等待上游确认…成功后这一家的账号会自动出现。"
                    pollJob?.cancel()
                    pollJob = scope.launch { pollLoop(s) }
                }
                is AdminClient.Result.Err -> errorText = r.message
            }
            starting = false
        }
    }

    // 有授权入口就一进来就开一次：弹窗里第一步应该是「打开授权页面」，
    // 而不是让用户再点一次。贴 Key 的形态不走这里。
    LaunchedEffect(channelId) {
        if (hasWebLogin) start()
    }

    Dialog(
        onDismissRequest = { stopPolling(); onClose() },
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Box(
            Modifier
                .fillMaxSize()
                .background(Lamp.colors.overlay)
                .clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                ) { stopPolling(); onClose() },
            contentAlignment = Alignment.Center,
        ) {
            Column(
                Modifier
                    .fillMaxWidth()
                    .widthIn(max = 520.dp)
                    .padding(horizontal = 18.dp)
                    .clip(RoundedCornerShape(Radius.FLYOUT.dp))
                    .background(Lamp.colors.surfaceSolid)
                    .border(1.dp, Lamp.colors.lineStrong, RoundedCornerShape(Radius.FLYOUT.dp))
                    .clickable(
                        interactionSource = remember { MutableInteractionSource() },
                        indication = null,
                    ) {},
            ) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .padding(start = 18.dp, end = 12.dp, top = 14.dp, bottom = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            "添加账号 · " + (channel?.name ?: channelId),
                            color = Lamp.colors.text,
                            style = MaterialTheme.typography.titleMedium,
                        )
                        Text(
                            if (hasWebLogin) "按提示在浏览器里完成授权，成功后账号自动出现" else "这一家只能贴 Key",
                            color = Lamp.colors.textDim,
                            style = MaterialTheme.typography.labelSmall,
                        )
                    }
                    GhostButton("关闭") { stopPolling(); onClose() }
                }
                Box(Modifier.fillMaxWidth().height(1.dp).background(Lamp.colors.line))

                Column(
                    Modifier
                        .fillMaxWidth()
                        .heightIn(max = 460.dp)
                        .verticalScroll(rememberScrollState())
                        .padding(horizontal = 18.dp, vertical = 14.dp),
                ) {
                    if (hasWebLogin) {
                        Text(
                            KIND_STEP[kind]
                                ?: "按下面的提示在浏览器里完成授权；成功后这一家的账号会自动出现在清单里。",
                            color = Lamp.colors.textDim,
                            style = MaterialTheme.typography.bodySmall,
                        )

                        val s = session
                        if (s != null) {
                            if (s.qrImage.isNotBlank()) {
                                Spacer(Modifier.height(12.dp))
                                RemoteImage(s.qrImage, Modifier.fillMaxWidth().height(220.dp))
                            }
                            if (s.userCode.isNotBlank()) {
                                Spacer(Modifier.height(12.dp))
                                Text(
                                        "把这段码填进授权页面",
                                        color = Lamp.colors.textDim,
                                        style = MaterialTheme.typography.labelSmall,
                                )
                                Spacer(Modifier.height(5.dp))
                                Box(
                                    Modifier
                                        .fillMaxWidth()
                                        .clip(RoundedCornerShape(Radius.CONTROL.dp))
                                        .background(Lamp.colors.surfaceHigh)
                                        .border(1.dp, Lamp.colors.amber.copy(alpha = 0.45f), RoundedCornerShape(Radius.CONTROL.dp))
                                        .padding(horizontal = 14.dp, vertical = 12.dp),
                                ) {
                                    Text(
                                        s.userCode,
                                        color = Lamp.colors.text,
                                        style = MaterialTheme.typography.titleMedium.copy(fontFamily = FontFamily.Monospace),
                                    )
                                }
                                Spacer(Modifier.height(8.dp))
                                GhostButton("复制这段码") { onCopy("授权码", s.userCode) }
                            }
                            if (s.openUrl.isNotBlank()) {
                                Spacer(Modifier.height(12.dp))
                                Text("授权页面", color = Lamp.colors.textDim, style = MaterialTheme.typography.labelSmall)
                                Spacer(Modifier.height(5.dp))
                                Text(
                                    s.openUrl,
                                    color = Lamp.colors.textDim,
                                    style = MaterialTheme.typography.bodySmall,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Spacer(Modifier.height(8.dp))
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    PrimaryButton("打开授权页面", onClick = { onOpenUrl(s.openUrl) })
                                    GhostButton("复制链接") { onCopy("授权链接", s.openUrl) }
                                }
                            }
                            if (s.detail.isNotBlank()) {
                                Spacer(Modifier.height(10.dp))
                                Text(s.detail, color = Lamp.colors.textFaint, style = MaterialTheme.typography.labelSmall)
                            }
                            if (s.expiresAtMs > 0) {
                                Spacer(Modifier.height(8.dp))
                                Text(
                                        "这个授权窗口到 " + Fmt.stamp(s.expiresAtMs) + " 结束",
                                        color = Lamp.colors.textFaint,
                                        style = MaterialTheme.typography.labelSmall,
                                )
                            }
                            Spacer(Modifier.height(12.dp))
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                if (polling) {
                                    CircularProgressIndicator(
                                            Modifier.size(16.dp),
                                            color = Lamp.colors.amber,
                                            strokeWidth = 2.dp,
                                    )
                                    Spacer(Modifier.width(10.dp))
                                }
                                Text(
                                    statusText.ifBlank { "正在等待上游确认…" },
                                    color = if (statusText.startsWith("已登录")) Lamp.colors.ok else Lamp.colors.textDim,
                                    style = MaterialTheme.typography.bodySmall,
                                    modifier = Modifier.weight(1f),
                                )
                            }
                            Spacer(Modifier.height(8.dp))
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                GhostButton(if (starting) "正在开始…" else "重新开始") { start() }
                            }
                        } else if (!starting && errorText.isBlank()) {
                            Spacer(Modifier.height(12.dp))
                            PrimaryButton("开始登录", onClick = { start() })
                        }

                        if (errorText.isNotBlank()) {
                            Spacer(Modifier.height(10.dp))
                            Text(errorText, color = Lamp.colors.bad, style = MaterialTheme.typography.bodySmall)
                        }

                        Spacer(Modifier.height(14.dp))
                        GhostButton(if (showPaste) "收起贴 Key" else "也可以直接贴 Key") { showPaste = !showPaste }
                    }

                    if (showPaste) {
                        Spacer(Modifier.height(12.dp))
                        Text(
                            loginSpec?.note.orEmpty().ifBlank { "抄一份 session / token 贴进来，比走一遍授权更快。" },
                            color = Lamp.colors.textFaint,
                            style = MaterialTheme.typography.labelSmall,
                        )
                        Spacer(Modifier.height(8.dp))
                        LampField(
                            value = token,
                            onValueChange = { token = it },
                            label = loginSpec?.pasteLabel.orEmpty().ifBlank { "粘贴 Key" },
                            placeholder = loginSpec?.pasteHint.orEmpty().ifBlank { "把 Key 粘在这里" },
                            singleLine = false,
                        )
                        Spacer(Modifier.height(8.dp))
                        PrimaryButton(
                            "保存 Key",
                            onClick = { vm.pasteToken(channelId, token.trim()) },
                            enabled = token.isNotBlank(),
                        )
                    }
                }
            }
        }
    }
}

/**
 * 远程图（二维码）。不引 Coil：一张几 KB 的 png，一次 decodeStream 就够。
 * 拉不到就显示一句话 —— 二维码画不出来时，授权页与设备码仍然可用。
 */
@Composable
private fun RemoteImage(url: String, modifier: Modifier = Modifier) {
    var bmp by remember(url) { mutableStateOf<ImageBitmap?>(null) }
    LaunchedEffect(url) {
        bmp = withContext(Dispatchers.IO) {
            runCatching {
                val conn = java.net.URL(url).openConnection()
                conn.connectTimeout = 8000
                conn.readTimeout = 8000
                conn.getInputStream().use { BitmapFactory.decodeStream(it) }?.asImageBitmap()
            }.getOrNull()
        }
    }
    Box(
        modifier
            .clip(RoundedCornerShape(Radius.CARD.dp))
            .background(Lamp.colors.surfaceHigh)
            .border(1.dp, Lamp.colors.line, RoundedCornerShape(Radius.CARD.dp)),
        contentAlignment = Alignment.Center,
    ) {
        val image = bmp
        if (image != null) {
            Image(
                image,
                contentDescription = "扫码登录",
                modifier = Modifier.fillMaxSize(),
                contentScale = ContentScale.Fit,
            )
        } else {
            Text("二维码加载中…", color = Lamp.colors.textFaint, style = MaterialTheme.typography.labelSmall)
        }
    }
}
