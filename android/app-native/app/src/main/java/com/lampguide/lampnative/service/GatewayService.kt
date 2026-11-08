package com.lampguide.lampnative.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.lampguide.lampnative.Gateway
import com.lampguide.lampnative.MainActivity
import com.lampguide.lampnative.PeerReport
import com.lampguide.lampnative.R

/**
 * GatewayService 是安卓上「托盘」的等价物：START_STICKY 前台服务，进程内跑
 * Go 网关，常驻通知是它唯一可视面。
 *
 * 它不做业务判断——跑的是与桌面版相同的 `core.Start`，只是
 * `DisableDesktop` 裁掉了桌面专属的那几项。它负责的四件事都在第 6 节里：
 *
 * - §6.2 端口被占 = 静默死：重试一次，仍失败把原文抬到通知 + 首屏，不换端口。
 * - §6.5 dataSync 6 小时上限：实现两个 onTimeout，改通知为「需要重新打开」。
 * - §6.6 电池优化：设置页入口，服务这里不纠缠。
 * - §6.8 重装即清 Key：通知与页面都不缓存 Key。
 */
class GatewayService : Service() {

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIF_ID, buildNotification(State.STARTING, "", ""))
        runningAtStartId = startId
        /* 进程被 START_STICKY 重建时 Activity 可能不在栈上：同机应用的报告要
           在这里补一次，否则页面会从「已安装」退回「未上报」——一个本来能答
           的事实不该因为进程重启而变未知。 */
        PeerReport.report(this)

        when (val boot = Gateway.start(this, Gateway.DEFAULT_PORT)) {
            is Gateway.Boot.Up -> {
                lastBaseURL = boot.baseURL
                state = State.UP
                refreshNotification()
            }
            is Gateway.Boot.Down -> {
                lastBaseURL = ""
                state = State.FAILED
                // 原文上屏：只说「启动失败」会让人无从自救。
                failureText = boot.reason
                refreshNotification()
            }
        }
        return START_STICKY
    }

    /**
     * §6.5：Android 15+ 的 dataSync 上限是 6 小时，超时系统会回调这里。
     * 不实现它就是被静默杀掉——用户切回来看见页面还开着，网关却没了。
     */
    override fun onTimeout(startId: Int) {
        onTimeoutInternal("数据同步前台服务已到 6 小时上限")
    }

    override fun onTimeout(startId: Int, fgsType: Int) {
        onTimeoutInternal("数据同步前台服务已到 6 小时上限（类型 $fgsType）")
    }

    private var runningAtStartId = 0

    private fun onTimeoutInternal(reason: String) {
        Log.w(TAG, "onTimeout: $reason")
        // 通知改成「需要重新打开」：这是系统限制，不是网关故障。
        state = State.TIMEOUT
        failureText = reason + "，网关已停止；点按这个通知重新打开应用即可恢复。"
        lastBaseURL = ""
        refreshNotification()
        stopForeground(STOP_FOREGROUND_DETACH)
        stopSelf(runningAtStartId)
    }

    override fun onDestroy() {
        Gateway.stop()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // ------------------------------------------------------------------ 通知

    private fun refreshNotification() {
        val (title, text) = when (state) {
            State.UP -> "网关运行中" to lastBaseURL.ifBlank { Gateway.baseURL() }
            State.FAILED -> "网关启动失败" to failureText
            State.TIMEOUT -> "需要重新打开" to failureText
            State.STARTING -> "正在启动…" to ""
        }
        val n = buildNotification(state, title, text)
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIF_ID, n)
    }

    private fun buildNotification(s: State, title: String, text: String): Notification {
        // 只有一个 action：「打开」。不做「停止」——用户会误触关掉网关然后以为坏了。
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            },
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val builder = NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(text.ifBlank { "点按打开" })
            .setStyle(NotificationCompat.BigTextStyle().bigText(text.ifBlank { "点按打开" }))
            .setSmallIcon(R.drawable.ic_stat_lamp)
            .setContentIntent(open)
            .setOngoing(s == State.UP || s == State.STARTING)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
        if (s == State.FAILED || s == State.TIMEOUT) {
            // 失败要能被看见：os 的低优先级渠道会把它压到底部。
            builder.setPriority(NotificationCompat.PRIORITY_DEFAULT)
        } else {
            builder.setPriority(NotificationCompat.PRIORITY_LOW)
        }
        return builder.build()
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = getSystemService(NotificationManager::class.java)
        val ch = NotificationChannel(
            CHANNEL_ID,
            "Lamp Guide 网关",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "网关运行状态（常驻通知，替代桌面托盘）"
            setShowBadge(false)
        }
        nm.createNotificationChannel(ch)
    }

    enum class State { STARTING, UP, FAILED, TIMEOUT }

    companion object {
        const val TAG = "LampGateway"
        const val CHANNEL_ID = "lamp-gateway"
        const val NOTIF_ID = 1101

        @Volatile private var state: State = State.STARTING
        @Volatile private var lastBaseURL: String = ""
        @Volatile private var failureText: String = ""

        /** 页面切回前台时读一次，用来决定首屏是状态卡还是红块。 */
        fun snapshot(): Triple<State, String, String> = Triple(state, lastBaseURL, failureText)

        fun start(context: Context) {
            ContextCompat.startForegroundService(
                context, Intent(context, GatewayService::class.java),
            )
        }

        /** 设置页的「重启网关」：停掉当前监听再起，端口改动才会生效。 */
        fun restart(context: Context) {
            Gateway.stop()
            state = State.STARTING
            lastBaseURL = ""
            failureText = ""
            start(context)
        }
    }
}
