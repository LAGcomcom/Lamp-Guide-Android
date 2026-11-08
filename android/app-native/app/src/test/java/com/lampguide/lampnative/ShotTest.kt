package com.lampguide.lampnative

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import android.graphics.Canvas
import android.os.Looper
import android.view.View
import org.robolectric.Robolectric
import org.robolectric.Shadows.shadowOf
import com.lampguide.lampnative.data.AgentRow
import com.lampguide.lampnative.data.CallRecord
import com.lampguide.lampnative.data.ChannelAccount
import com.lampguide.lampnative.data.ChannelRow
import com.lampguide.lampnative.data.ChannelState
import com.lampguide.lampnative.data.ChannelTotals
import com.lampguide.lampnative.data.CheckinResult
import com.lampguide.lampnative.data.CheckinRow
import com.lampguide.lampnative.data.CheckinSettings
import com.lampguide.lampnative.data.CheckinView
import com.lampguide.lampnative.data.DayStat
import com.lampguide.lampnative.data.LoginSpec
import com.lampguide.lampnative.data.ModelRow
import com.lampguide.lampnative.data.Quota
import com.lampguide.lampnative.data.SettingsView
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.data.Stats
import com.lampguide.lampnative.data.UiError
import com.lampguide.lampnative.ui.AppActions
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.screens.S1Status
import com.lampguide.lampnative.ui.screens.S2Channels
import com.lampguide.lampnative.ui.screens.S3Connect
import com.lampguide.lampnative.ui.screens.S4Models
import com.lampguide.lampnative.ui.screens.S5Usage
import com.lampguide.lampnative.ui.screens.S6Settings
import com.lampguide.lampnative.ui.theme.Lamp
import com.lampguide.lampnative.ui.theme.LampTheme
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * 截图取证。真机不可用时（这台开发机上没有 adb 设备、也没有模拟器镜像），
 * 用 Robolectric 的 NATIVE 图形模式把真实的 Compose 组件画到 Bitmap 上，
 * 存进 shots/native/。它渲染的是**同一份界面代码**，不是另画的示意图。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = "w411dp-h891dp-xxhdpi")
class ShotTest {



    private val out = File(System.getProperty("lampguide.shots.dir", "build/outputs/screenshots"))

    // ------------------------------------------------------------------ 假数据

    private val models = List(55) { i ->
        val states = listOf("available", "throttled", "unavailable", "unknown", "region-blocked")
        ModelRow(
            id = "model-$i",
            name = if (i == 0) "deepseek-v3.2" else "model-$i",
            state = states[i % states.size],
            ttftMedMs = if (i % 3 == 0) 620L else 0,
            contextWindow = 128000,
            quotaUsed = i * 1200,
        )
    }

    private val nowIso = "2026-11-08 09:20:00"

    private val days = buildMap {
        // 11-02 … 11-08：最后一天必须是「今天」，S1/S5 才有当日数字。
        for (d in 2..8) {
            put(
                "2026-11-0$d",
                DayStat(
                    requests = d * 40,
                    failed = d,
                    input = d * 900,
                    output = d * 2200,
                    models = mapOf("deepseek-v3.2" to d * 1800, "model-3" to d * 400),
                    modelReqs = mapOf("deepseek-v3.2" to d * 20, "model-3" to d * 5),
                ),
            )
        }
    }

    private val state = StateView(
        baseURL = "http://127.0.0.1:8787/v1",
        lanBaseURL = "http://192.168.1.20:8787/v1",
        port = 8787,
        mainKey = "lamp_sk_0123456789abcdef",
        agentKeys = mapOf("dsh-go" to "lamp_sk_agent_9f8e7d6c"),
        models = models,
        agents = listOf(
            AgentRow(
                id = "dsh-go", name = "DSH Go", installed = true, version = "1.0.12",
                enabled = true,
                detail = "DSH Go 的接口配置存在它自己的私有目录里，安卓沙箱不允许本应用写另一个 App 的数据。",
            )
        ),
        stats = Stats(days = days, recent = List(6) { CallRecord(model = "model-3", input = 900, output = 1200, ok = true) }),
        settings = SettingsView(
            defaultMaxTokens = 4096, defaultEffort = "balanced", probeIntervalMinutes = 10,
            proxyMode = "direct", allowLan = true, logLevel = "info",
        ),
        version = "1.4.2",
        startedAt = "2026-11-08 02:11:05",
        uptime = "7h9m",
        uptimeSec = 25755,
        dataDir = "/data/user/0/com.lampguide.lampnative/files/zen-gate",
        now = nowIso,
    )

    private val channels = ChannelState(
        channels = listOf(
            ChannelRow(
                id = "deepseek", name = "DeepSeek", accountCount = 2, accounts = 2, loggedIn = true,
                accountList = listOf(
                    ChannelAccount(key = "a1", label = "主账号", nickname = "lamp-main"),
                    ChannelAccount(key = "a2", label = "备用", nickname = "lamp-backup"),
                ),
                quota = Quota(known = true, remaining = JsonPrimitive("1.2M")),
                login = LoginSpec(kind = "device", deviceUrl = "https://example.com/device", pasteLabel = "粘贴 Key"),
            ),
            ChannelRow(
                id = "zhipu", name = "智谱", accountCount = 0, loggedIn = false,
                login = LoginSpec(kind = "paste", pasteLabel = "粘贴 API Key", pasteHint = "id.secret"),
            ),
        ),
        totals = ChannelTotals(channels = 2, accounts = 2, loggedIn = 1),
    )

    private val checkin = CheckinView(
        settings = CheckinSettings(enabled = true, times = listOf("08:00")),
        timezone = "Asia/Shanghai",
        today = "2026-11-08",
        running = false,
        accounts = listOf(
            CheckinRow(
                channel = "deepseek", key = "a1", label = "主账号", mode = "auto", enabled = true,
                result = CheckinResult(
                    status = "success", message = "签到成功", day = "2026-11-08",
                    updatedAt = 1794098000000,
                ),
            ),
            CheckinRow(
                channel = "zhipu", key = "", label = "智谱", mode = "unsupported",
                reason = "这一家没有签到接口",
            ),
        ),
    )

    // ------------------------------------------------------------------ 渲染

    /**
     * 真机不可用时的取证路径：把真实的 Compose 树挂进一个 Robolectric Activity，
     * 走完测量/布局/绘制，再把 DecorView 画到 Bitmap。
     *
     * 不用 createComposeRule：它内部的 waitForIdle 依赖 Robolectric 的自动时钟，
     * 在这套 Compose 版本上会稳态超时；直接借 Activity 的窗口反而更接近真机。
     */
    private fun shot(name: String, dark: Boolean, content: @Composable () -> Unit) {
        val controller = Robolectric.buildActivity(ComponentActivity::class.java).setup()
        val activity = controller.get()
        activity.setContent {
            LampTheme(darkTheme = dark) {
                Box(Modifier.fillMaxSize().background(Lamp.colors.ground)) { content() }
            }
        }
        shadowOf(Looper.getMainLooper()).idle()

        val view: View = activity.window.decorView
        val w = view.width.takeIf { it > 0 } ?: 1080
        val h = view.height.takeIf { it > 0 } ?: 2340
        view.measure(
            View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY),
            View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY),
        )
        view.layout(0, 0, w, h)
        shadowOf(Looper.getMainLooper()).idle()

        val bmp = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
        view.draw(Canvas(bmp))
        out.mkdirs()
        File(out, name).outputStream().use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        controller.pause().stop().destroy()
    }

    private val noop = object : AppActions {
        override fun copy(label: String, text: String) = "$label 已复制"
        override fun openUrl(url: String) = "已在浏览器打开授权页面"
        override fun openDshGo() {}
        override fun restartGateway() {}
        override fun requestBatteryExemption() {}
        override fun loadErrors() {}
        override fun keyDrifted() = true
        override val errors: Load<List<String>> = Load.Empty
    }

    @Test fun s1Dark() = shot("s1-dark.png", true) {
        S1Status(Load.Data(state), {}, {}, {}, {}, { _, _ -> })
    }

    @Test fun s1LightError() = shot("s1-light-error.png", false) {
        S1Status(
            Load.Error(UiError("gateway_down", "listen tcp 127.0.0.1:8787: bind: Only one usage of each socket address is normally permitted.")),
            {}, {}, {}, {}, { _, _ -> },
        )
    }

    @Test fun s1Empty() = shot("s1-dark-empty.png", true) {
        S1Status(Load.Empty, {}, {}, {}, {}, { _, _ -> })
    }

    @Test fun s2Dark() = shot("s2-dark.png", true) {
        S2Channels(Load.Data(channels), Load.Data(checkin), {}, {}, { _, _ -> })
    }

    /** 展开一家渠道：账号表在折叠时看不见，折叠态截图验不到它。 */
    @Test fun s2DarkExpanded() = shot("s2-dark-expanded.png", true) {
        S2Channels(Load.Data(channels), Load.Data(checkin), {}, {}, { _, _ -> }, openFirst = true)
    }

    @Test fun s2LightExpanded() = shot("s2-light-expanded.png", false) {
        S2Channels(Load.Data(channels), Load.Data(checkin), {}, {}, { _, _ -> }, openFirst = true)
    }

    @Test fun s2Empty() = shot("s2-dark-empty.png", true) {
        S2Channels(Load.Empty, Load.Empty, {}, {}, { _, _ -> })
    }

    @Test fun s3Dark() = shot("s3-dark.png", true) {
        S3Connect(Load.Data(state), keyDrifted = true, onToggle = {}, onOpenDshGo = {}, onCopy = { _, _ -> })
    }

    @Test fun s4Dark() = shot("s4-dark.png", true) {
        S4Models(Load.Data(state), Load.Empty, { _, _ -> }, { _, _ -> })
    }

    @Test fun s5Dark() = shot("s5-dark.png", true) {
        S5Usage(Load.Data(state))
    }

    @Test fun s5Empty() = shot("s5-dark-empty.png", true) {
        S5Usage(Load.Empty)
    }

    // ---- 浅色：强调色在浅底上是 #005FB8，表格线与卡片描边是黑的一档 0.058 白，
    // 与深色是两套对照关系，只在深色上验收等于只验了一半。

    @Test fun s1Light() = shot("s1-light.png", false) {
        S1Status(Load.Data(state), {}, {}, {}, {}, { _, _ -> })
    }

    @Test fun s2Light() = shot("s2-light.png", false) {
        S2Channels(Load.Data(channels), Load.Data(checkin), {}, {}, { _, _ -> })
    }

    @Test fun s4Light() = shot("s4-light.png", false) {
        S4Models(Load.Data(state), Load.Empty, { _, _ -> }, { _, _ -> })
    }

    /**
     * 自建渠道的模型：状态是「待测」，曾经因为默认筛选只认 available 而整条
     * 消失——那正是"我明明在 Qoder 里加了 DeepSeek 却看不见"的那一屏。
     */
    @Test fun s4Pending() = shot("s4-pending.png", true) {
        val withCustom = state.copy(
            models = state.models + listOf(
                ModelRow(
                    id = "qoder/DeepSeek-Flash", name = "DeepSeek-Flash", state = "custom",
                    providerId = "qoder", contextWindow = 180000, source = "Qoder",
                ),
                ModelRow(
                    id = "deepseek-v4.1-flash", name = "deepseek-v4.1-flash",
                    state = "custom", channel = "workbuddy", providerId = "workbuddy",
                    source = "WorkBuddy",
                ),
            ),
        )
        S4Models(Load.Data(withCustom), Load.Empty, { _, _ -> }, { _, _ -> })
    }

    /**
     * 同一个 id 在两条 lane 里各有一条：`deepseek-v4.1-flash` 免费车道一条、
     * WorkBuddy 一条。只按 id 做 LazyColumn 的 key 会撞成重复键并抛异常，
     * 这一屏就是那条回归线。
     */
    @Test fun s4SameIdTwoSources() = shot("s4-same-id.png", true) {
        val dup = state.copy(
            models = state.models + listOf(
                ModelRow(id = "deepseek-v4.1-flash", name = "DeepSeek V4.1 Flash", state = "unknown"),
                ModelRow(
                    id = "deepseek-v4.1-flash", name = "DeepSeek V4.1 Flash",
                    state = "available", channel = "workbuddy", source = "WorkBuddy",
                    contextWindow = 1000000,
                ),
            ),
        )
        S4Models(Load.Data(dup), Load.Empty, { _, _ -> }, { _, _ -> })
    }

    @Test fun s5Light() = shot("s5-light.png", false) {
        S5Usage(Load.Data(state))
    }

    @Test fun s6Light() = shot("s6-light.png", false) {
        S6Settings(
            Load.Data(state), "1.4.2", noop.errors,
            { _, _, _ -> }, {}, {}, {}, {}, {},
        )
    }

    @Test fun s6Dark() = shot("s6-dark.png", true) {
        S6Settings(
            Load.Data(state), "1.4.2", noop.errors,
            { _, _, _ -> }, {}, {}, {}, {}, {},
        )
    }
}
