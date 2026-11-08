package com.lampguide.lampnative.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * 网关 admin 路由的 JSON 形状，逐字对齐 `gateway/internal/zen/gateway` 里的
 * 结构体标签。**这里不放任何业务判断**：字段缺失就用默认值，界面对缺失一律
 * 显示「—」而不是 0，因为「网关没给」与「真的为零」是两件事。
 */

@Serializable
data class Envelope(
    val ok: Boolean = false,
    val code: String? = null,
    val error: String? = null,
    val data: JsonElement? = null,
)

@Serializable
data class ModelRow(
    val id: String = "",
    val name: String = "",
    val blurb: String = "",
    val state: String = "unknown",
    val detail: String = "",
    val ttftMs: Long = 0,
    val latencyMs: Long = 0,
    val vision: Boolean = false,
    val reasoning: Boolean = false,
    val systemOne: Boolean = false,
    val contextWindow: Int = 0,
    val maxOutput: Int = 0,
    val quotaUsed: Int = 0,
    val quotaEstimate: Int = 0,
    val throttledAt: Long = 0,
    val recoverEta: Long = 0,
    val ttftMedMs: Long = 0,
    val ttftCount: Int = 0,
    val custom: Boolean = false,
    val providerId: String = "",
    val hidden: Boolean = false,
    /**
     * 这条模型来自哪条上游 lane（workbuddy / kilo / 空=免费车道）。
     *
     * 同一个 id 会在不同来源里各有一条（`deepseek-v4.1-flash` 免费车道一条、
     * WorkBuddy 一条），只按 id 做 key 会让列表在两个相同 key 上崩掉。
     */
    val channel: String = "",
    /** 给「来源」列用的显示名；免费车道留空。 */
    val source: String = "",
)

@Serializable
data class AgentRow(
    val id: String = "",
    val name: String = "",
    val detail: String = "",
    val installed: Boolean = false,
    val version: String = "",
    val enabled: Boolean = false,
    val warn: String = "",
)

@Serializable
data class DayStat(
    val requests: Int = 0,
    val failed: Int = 0,
    val input: Int = 0,
    val output: Int = 0,
    val models: Map<String, Int> = emptyMap(),
    val modelReqs: Map<String, Int> = emptyMap(),
    val agents: Map<String, Int> = emptyMap(),
)

@Serializable
data class CallRecord(
    val model: String = "",
    val agent: String = "",
    val ok: Boolean = false,
    val input: Int = 0,
    val output: Int = 0,
    val ttftMs: Long = 0,
    val effort: String = "",
    val at: Long = 0,
    val errCode: String = "",
)

@Serializable
data class Stats(
    val days: Map<String, DayStat> = emptyMap(),
    val recent: List<CallRecord> = emptyList(),
)

@Serializable
data class SettingsView(
    val defaultMaxTokens: Int = 0,
    val defaultEffort: String = "",
    val probeIntervalMinutes: Int = 0,
    val proxyMode: String = "",
    val proxyURL: String = "",
    val allowLan: Boolean = false,
    val logLevel: String = "",
    val logKeepDays: Int = 0,
    val aggregateEnabled: Boolean = false,
)

@Serializable
data class StateView(
    val baseURL: String = "",
    val lanBaseURL: String = "",
    val port: Int = 0,
    val mainKey: String = "",
    val agentKeys: Map<String, String> = emptyMap(),
    val models: List<ModelRow> = emptyList(),
    val agents: List<AgentRow> = emptyList(),
    val stats: Stats = Stats(),
    val settings: SettingsView = SettingsView(),
    val version: String = "",
    val startedAt: String = "",
    val uptime: String = "",
    val uptimeSec: Long = 0,
    val dataDir: String = "",
    val now: String = "",
    val probingModel: String = "",
)

@Serializable
data class Quota(
    val known: Boolean = false,
    val never: Boolean = false,
    val failed: Boolean = false,
    val expiryMs: Long = 0,
    val detail: String = "",
    val remaining: JsonElement? = null,
)

@Serializable
data class ChannelAccount(
    val key: String = "",
    val label: String = "",
    val source: String = "",
    val id: String = "",
    val nickname: String = "",
    val createdAtMs: Long = 0,
    val expiresAtMs: Long = 0,
    val extraKeys: List<String> = emptyList(),
    val quota: Quota? = null,
)

@Serializable
data class LoginSpec(
    val kind: String = "",
    val pasteLabel: String = "",
    val pasteHint: String = "",
    val note: String = "",
    val deviceUrl: String = "",
    val authUrl: String = "",
    val startUrl: String = "",
    val qrUrl: String = "",
)

@Serializable
data class QuotaSpec(
    val baseUrl: String = "",
    val path: String = "",
    val method: String = "",
    val auth: String = "",
)

@Serializable
data class ChannelRow(
    val id: String = "",
    val name: String = "",
    val vendor: String = "",
    val protocol: String = "",
    val tier: String = "",
    val creditUnit: String = "",
    val dailyClaim: Boolean = false,
    val note: String = "",
    val loggedIn: Boolean = false,
    val routable: Boolean = false,
    val accounts: Int = 0,
    val accountCount: Int = 0,
    val accountList: List<ChannelAccount> = emptyList(),
    val single: Boolean = false,
    val quota: Quota = Quota(),
    val login: LoginSpec = LoginSpec(),
    val spec: QuotaSpec = QuotaSpec(),
    val catalogCount: Int = 0,
    val catalogError: String = "",
)

@Serializable
data class ChannelTotals(
    val channels: Int = 0,
    val wired: Int = 0,
    val loggedIn: Int = 0,
    val withQuota: Int = 0,
    val expiringSoon: Int = 0,
    val accounts: Int = 0,
)

@Serializable
data class ChannelState(
    val channels: List<ChannelRow> = emptyList(),
    val totals: ChannelTotals = ChannelTotals(),
)

@Serializable
data class CheckinSettings(
    val enabled: Boolean = true,
    val times: List<String> = emptyList(),
    val disabled: Map<String, Boolean> = emptyMap(),
)

@Serializable
data class CheckinResult(
    val status: String = "",
    val message: String = "",
    val coveredDay: String = "",
    val amount: Double? = null,
    val day: String = "",
    val slot: String = "",
    val attempts: Int = 0,
    val updatedAt: Long = 0,
    val nextAttemptAt: Long = 0,
)

@Serializable
data class CheckinRow(
    val channel: String = "",
    val key: String = "",
    val label: String = "",
    val mode: String = "",
    val reason: String = "",
    val enabled: Boolean = false,
    val result: CheckinResult = CheckinResult(),
)

@Serializable
data class CheckinView(
    val settings: CheckinSettings = CheckinSettings(),
    val timezone: String = "",
    val today: String = "",
    val running: Boolean = false,
    val error: String = "",
    val accounts: List<CheckinRow> = emptyList(),
)

@Serializable
data class AggregateCandidate(
    val channel: String = "",
    val realId: String = "",
    val realName: String = "",
    val eligible: Boolean = false,
    val wired: Boolean = false,
)

@Serializable
data class VirtualModel(
    val key: String = "",
    val name: String = "",
    val candidates: List<AggregateCandidate> = emptyList(),
)

@Serializable
data class AggregateTotals(
    val participatingChannels: Int = 0,
    val wiredChannels: Int = 0,
    val virtualModels: Int = 0,
    val candidates: Int = 0,
    val expiringSoon: Int = 0,
)

@Serializable
data class AggregateState(
    val enabled: Boolean = false,
    val virtual: List<VirtualModel> = emptyList(),
    val totals: AggregateTotals = AggregateTotals(),
)

@Serializable
data class LogEntry(
    val at: String = "",
    val level: String = "",
    val cat: String = "",
    val msg: String = "",
)

@Serializable
data class LogView(
    val entries: List<LogEntry> = emptyList(),
    val days: List<String> = emptyList(),
)

/** chprov.LoginSession：面板要渲染的三件事 —— 现在该干什么、还剩多久、为什么没有。 */
@Serializable
data class LoginSession(
    val channel: String = "",
    val kind: String = "",
    val openUrl: String = "",
    val userCode: String = "",
    val qrImage: String = "",
    val intervalMs: Long = 0,
    val expiresAtMs: Long = 0,
    val detail: String = "",
)

@Serializable
data class PollResult(
    val state: String = "",
    val added: Boolean = false,
)

@Serializable
data class LoginResponse(
    val session: LoginSession = LoginSession(),
    val kind: String = "",
)
