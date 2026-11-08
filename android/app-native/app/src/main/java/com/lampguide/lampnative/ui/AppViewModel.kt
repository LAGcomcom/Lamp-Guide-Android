package com.lampguide.lampnative.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.lampguide.lampnative.data.AdminClient
import com.lampguide.lampnative.data.AggregateState
import com.lampguide.lampnative.data.ChannelState
import com.lampguide.lampnative.data.CheckinView
import com.lampguide.lampnative.data.EnvelopeCodes
import com.lampguide.lampnative.data.StateView
import com.lampguide.lampnative.data.UiError
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * 一个 ViewModel 管全应用的数据：六屏读的是同一份 state，分成六个仓库只会
 * 让「刷新一下」变成六次请求。
 *
 * `state` 是单一事实源（缓存 5 秒，与桌面版同档）；`channel/state`、
 * `checkin/state`、`aggregate/state` 只在渠道页与模型页需要时才拉。
 */
class AppViewModel(app: Application) : AndroidViewModel(app) {

    private val client = AdminClient()

    private val _state = MutableStateFlow<Load<StateView>>(Load.Loading)
    val state: StateFlow<Load<StateView>> = _state.asStateFlow()

    private val _channels = MutableStateFlow<Load<ChannelState>>(Load.Loading)
    val channels: StateFlow<Load<ChannelState>> = _channels.asStateFlow()

    private val _checkin = MutableStateFlow<Load<CheckinView>>(Load.Loading)
    val checkin: StateFlow<Load<CheckinView>> = _checkin.asStateFlow()

    private val _aggregate = MutableStateFlow<Load<AggregateState>>(Load.Loading)
    val aggregate: StateFlow<Load<AggregateState>> = _aggregate.asStateFlow()

    /** 一次性提示（复制成功、命令原话）。界面读完就清。 */
    private val _toast = MutableStateFlow<String?>(null)
    val toast: StateFlow<String?> = _toast.asStateFlow()

    private var stateAt = 0L
    private var inflight = false
    private var following = false

    /** 登录弹层：这次要登录哪一家；null 表示没有弹层。 */
    private val _loginChannel = MutableStateFlow<String?>(null)
    val loginChannel: StateFlow<String?> = _loginChannel.asStateFlow()

    /** 登录轮询要一个和 ViewModel 同寿命的作用域：界面关掉就一起停。 */
    val scope: kotlinx.coroutines.CoroutineScope get() = viewModelScope

    fun openLogin(channelId: String) { _loginChannel.value = channelId }
    fun closeLogin() { _loginChannel.value = null }

    fun notify(message: String) { _toast.value = message }
    fun consumeToast() { _toast.value = null }

    // -------------------------------------------------------------- 拉数据

    fun refreshAll(force: Boolean = true) {
        viewModelScope.launch {
            loadState(force)
            loadChannels()
            loadCheckin()
            loadAggregate()
        }
    }

    /**
     * 首屏启动序列。
     *
     * 真机上量出来的问题：Activity.onCreate 调 GatewayService.start() 之后立刻
     * refreshAll()，而 Go 那边 core.Start 要走 store.Open、建 55 个模型的 lane、
     * 起 checkin 循环才 listen —— 这中间有几百毫秒。首帧这次请求必然拿到
     * gateway_down，而此后**没有任何重试**，于是整个会话停在全屏红块上：
     * 网关明明在跑，界面却一直说它没起。
     *
     * 这里按 §6.2 的口径处理：gateway_down 是「还没起来」而不是「起不来」，
     * 所以退避重拉，直到拿到数据或确认失败。其余错误码不重试——业务拒绝
     * 再问一次结果也一样。
     */
    fun bootstrap() {
        viewModelScope.launch {
            // 必须自己 await：refreshAll() 内部又 launch 了一次，外面等它时
            // _state 还是 Loading，退避循环会在第一圈就判「不是 down」直接退出
            // ——这正是上一版补丁失效的原因。
            pullState()
            awaitGateway()
            loadChannels()
            loadCheckin()
            loadAggregate()
            followState()
        }
    }

    /**
     * 跟随网关自己的刷新节奏重拉 state。
     *
     * 真机上量出来的问题：bootstrap 只在首帧拉一次，而网关刚起来时目录还没补全
     * （实测首帧那次只有 23 个模型，全部 unknown；几十秒后同一路由已经报 501 个，
     * 其中 14 个可用）。界面于是长期停在「现在能用 0 / 共 23」——数字是真的，
     * 但它说的是**开机那一刻**的网关，不是现在的。
     *
     * 每 8 秒重拉一次：state 是进程内调用，没有网络开销，而模型可用性本来就按
     * 探测轮在变。只在拿到数据后开始；错误态不在这里循环（awaitGateway 管）。
     */
    fun followState() {
        if (following) return
        following = true
        viewModelScope.launch {
            while (true) {
                kotlinx.coroutines.delay(8_000)
                if (inflight) continue
                loadState(force = true)
                if (_channels.value is Load.Data || _channels.value is Load.Empty) loadChannels()
                loadAggregate()
            }
        }
    }

    /** 挂起地拉一次 state，把结果写进 _state。 */
    private suspend fun pullState() {
        when (val r = client.get<StateView>(AdminClient.Admin.STATE)) {
            is AdminClient.Result.Ok -> {
                android.util.Log.d("LampAdmin", "parsed models=${r.value.models.size} uptime=${r.value.uptimeSec}")
                _state.value = Load.Data(r.value)
                stateAt = now()
            }
            is AdminClient.Result.Err -> _state.value = Load.Error(UiError(r.code, r.message))
        }
    }

    /**
     * 网关由前台服务起，起完要几百毫秒（store.Open → 建 55 个模型的 lane →
     * 起签到循环 → listen）。首帧这次请求必然会撞上 gateway_down，而
     * gateway_down 是「还没起来」不是「起不来」，所以退避重拉。
     * 其余错误码不重试——业务拒绝再问一次结果也一样。
     */
    private suspend fun awaitGateway() {
        var waitMs = 300L
        repeat(24) {
            val down = (_state.value as? Load.Error)?.err?.code == EnvelopeCodes.GATEWAY_DOWN
            if (!down) return
            kotlinx.coroutines.delay(waitMs)
            waitMs = (waitMs + 400L).coerceAtMost(2500L)
            pullState()
        }
    }

    fun loadState(force: Boolean = false) {
        if (inflight) return
        val fresh = now() - stateAt
        if (!force && fresh in 0..5000L && _state.value is Load.Data) return
        inflight = true
        viewModelScope.launch {
            try {
                when (val r = client.get<StateView>(AdminClient.Admin.STATE)) {
                    is AdminClient.Result.Ok -> {
                        _state.value = Load.Data(r.value)
                        stateAt = now()
                    }
                    is AdminClient.Result.Err -> _state.value = Load.Error(UiError(r.code, r.message))
                }
            } finally {
                inflight = false
            }
        }
    }

    fun loadChannels() {
        viewModelScope.launch {
            when (val r = client.get<ChannelState>(AdminClient.Admin.CHANNEL_STATE)) {
                is AdminClient.Result.Ok -> {
                    val v = r.value
                    _channels.value = if (v.channels.isEmpty()) Load.Empty else Load.Data(v)
                }
                is AdminClient.Result.Err ->
                    _channels.value = Load.Error(UiError(r.code, r.message))
            }
        }
    }

    fun loadCheckin() {
        viewModelScope.launch {
            when (val r = client.get<CheckinView>(AdminClient.Admin.CHECKIN_STATE)) {
                is AdminClient.Result.Ok -> {
                    val v = r.value
                    _checkin.value = if (v.accounts.isEmpty()) Load.Empty else Load.Data(v)
                }
                is AdminClient.Result.Err ->
                    _checkin.value = Load.Error(UiError(r.code, r.message))
            }
        }
    }

    fun loadAggregate() {
        viewModelScope.launch {
            when (val r = client.get<AggregateState>(AdminClient.Admin.AGGREGATE_STATE)) {
                is AdminClient.Result.Ok -> _aggregate.value = Load.Data(r.value)
                is AdminClient.Result.Err ->
                    _aggregate.value = Load.Error(UiError(r.code, r.message))
            }
        }
    }

    // -------------------------------------------------------------- 写命令

    /** 签到：202 带 snapshot，busy 时 409 的原话直接上屏。 */
    fun runCheckin() {
        viewModelScope.launch {
            _checkin.value = _checkin.value.let {
                if (it is Load.Data) Load.Data(it.value.copy(running = true)) else it
            }
            when (val r = client.get<CheckinView>(AdminClient.Admin.CHECKIN_RUN)) {
                is AdminClient.Result.Ok -> {
                    _checkin.value = Load.Data(r.value)
                    notify("签到已经开始，本轮跑完会自动刷新")
                    loadChannels()
                    loadState(true)
                }
                is AdminClient.Result.Err -> {
                    notify(r.message) // 「签到正在执行，请等待本轮完成」就是这句
                    loadCheckin()
                }
            }
        }
    }

    /**
     * §6.4 提醒：`DisallowUnknownFields` 要全量 `{enabled,times,disabled}`。
     * 只改开关必须**读-改-写**，否则网关回 400「签到设置格式错误」。
     */
    fun setCheckinEnabled(enabled: Boolean) {
        val cur = (_checkin.value as? Load.Data)?.value ?: return
        viewModelScope.launch {
            val body = JsonObject(
                mapOf(
                    "enabled" to JsonPrimitive(enabled),
                    "times" to kotlinx.serialization.json.JsonArray(
                        cur.settings.times.map { JsonPrimitive(it) },
                    ),
                    "disabled" to JsonObject(
                        cur.settings.disabled.mapValues { JsonPrimitive(it.value) },
                    ),
                ),
            )
            when (val r = client.get<CheckinView>(AdminClient.Admin.CHECKIN_SETTINGS, body)) {
                is AdminClient.Result.Ok -> _checkin.value = Load.Data(r.value)
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun setAgentEnabled(id: String, enable: Boolean) {
        viewModelScope.launch {
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.agent(id, enable),
                JsonObject(emptyMap()),
            )) {
                is AdminClient.Result.Ok -> {
                    loadState(true)
                    notify(if (enable) "已开启 $id 的接入" else "已关闭 $id 的接入")
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun probe(modelId: String, channel: String = "") {
        viewModelScope.launch {
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.probe(modelId, channel), JsonObject(emptyMap()),
            )) {
                is AdminClient.Result.Ok -> {
                    notify("已探测 $modelId")
                    loadState(true)
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    /**
     * 把一批「待测」的模型挨个测一遍。
     *
     * 网关对自建渠道没有自动探测轮（只有免费车道有），所以这些模型会一直停在
     * state=custom，界面读起来就是"模型页里没有它"。串行发是因为每次探测都是一次
     * 真实上游请求：并发十条会同时打十个供应商，其中任何一条限流都会把整批打断。
     */
    fun probePending(targets: List<Pair<String, String>>) {
        if (targets.isEmpty()) return
        viewModelScope.launch {
            notify("开始测 ${targets.size} 个待测模型…")
            var done = 0
            for ((id, channel) in targets) {
                val r = client.get<kotlinx.serialization.json.JsonElement>(
                    AdminClient.Admin.probe(id, channel), JsonObject(emptyMap()),
                )
                done++
                if (r is AdminClient.Result.Err) {
                    notify("测 $id 失败：${r.message}")
                } else if (done == targets.size) {
                    notify("已测完 ${targets.size} 个")
                }
                loadState(true)
            }
        }
    }

    fun setModelHidden(id: String, hidden: Boolean) {
        viewModelScope.launch {
            val body = JsonObject(
                mapOf("id" to JsonPrimitive(id), "hidden" to JsonPrimitive(hidden)),
            )
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.MODELS_VISIBILITY, body,
            )) {
                is AdminClient.Result.Ok -> {
                    loadState(true)
                    notify(if (hidden) "已隐藏 $id" else "已显示 $id")
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun reprobe() {
        viewModelScope.launch {
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.REPROBE, JsonObject(emptyMap()),
            )) {
                is AdminClient.Result.Ok -> {
                    notify("全量探测已开始")
                    loadState(true)
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun rotateKey() {
        viewModelScope.launch {
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.KEY_ROTATE, JsonObject(emptyMap()),
            )) {
                is AdminClient.Result.Ok -> {
                    loadState(true)
                    notify("主 Key 已重新生成，所有已接入的客户端都要换 Key")
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    /** settings 是**部分更新**：只传要改的字段，未传不动。 */
    fun saveSettings(fields: Map<String, kotlinx.serialization.json.JsonElement>, note: String) {
        viewModelScope.launch {
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.SETTINGS, JsonObject(fields),
            )) {
                is AdminClient.Result.Ok -> {
                    loadState(true)
                    notify(note)
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun logout(channelId: String, key: String) {
        viewModelScope.launch {
            val body = JsonObject(
                mapOf("id" to JsonPrimitive(channelId), "key" to JsonPrimitive(key)),
            )
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.CHANNEL_LOGOUT, body,
            )) {
                is AdminClient.Result.Ok -> {
                    notify("已退出这个账号")
                    loadChannels()
                    loadCheckin()
                    loadState(true)
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    fun pasteToken(channelId: String, token: String) {
        viewModelScope.launch {
            val body = JsonObject(
                mapOf("id" to JsonPrimitive(channelId), "token" to JsonPrimitive(token)),
            )
            when (val r = client.get<kotlinx.serialization.json.JsonElement>(
                AdminClient.Admin.CHANNEL_TOKEN, body,
            )) {
                is AdminClient.Result.Ok -> {
                    notify("Key 已保存")
                    closeLogin()
                    loadChannels()
                    loadCheckin()
                    loadState(true)
                }
                is AdminClient.Result.Err -> notify(r.message)
            }
        }
    }

    /** 网关自己的版本串（`gateway.Version`），与 state.version 不同源。 */
    fun gatewayVersion(): String = com.lampguide.lampnative.Gateway.version()

    private fun now() = android.os.SystemClock.elapsedRealtime()

    /** 忘记 Key 指纹的记录键：DataStore 存的是 hash，不存 Key 本身。 */
    companion object {
        const val FINGERPRINT_KEY = "dsh_go_key_fingerprint"
        val EmptyCode = EnvelopeCodes.UNKNOWN
    }
}
