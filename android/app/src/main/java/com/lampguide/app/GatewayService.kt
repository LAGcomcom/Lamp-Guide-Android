package com.lampguide.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import mobile.Mobile

/**
 * GatewayService 是安卓上「托盘」的替代物：一个 START_STICKY 的前台服务，
 * 进程内跑 Go 网关，常驻通知是它唯一的可视面。
 *
 * 它不做任何业务判断——Mobile.start 里跑的是与桌面版完全相同的 core.Start，
 * 只是 Options.DisableDesktop 裁掉了桌面专属的那几项（自更新、Agent 配置注入、
 * sing-box 订阅 Apply、桌面弹窗）。
 */
class GatewayService : Service() {

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIF_ID, buildNotification("正在启动…", ""))
        /* 进程被 START_STICKY 重建时 Activity 可能不在栈上：同机应用的报告
           要在这里补一次，否则页面会从「已安装」退回「未上报」——一个本来
           能答的事实不该因为进程重启而变未知。 */
        PeerReport.report(this)

        if (!Mobile.isRunning()) {
            try {
                Mobile.start(homeDir(), MainActivity.PORT.toLong())
            } catch (e: Exception) {
                // 起不来是真实状态：通知如实说原因，页面也会看到 gateway_down。
                startForeground(NOTIF_ID, buildNotification("网关启动失败", e.message ?: e.toString()))
                return START_STICKY
            }
        }
        refresh()
        return START_STICKY
    }

    private fun refresh() {
        val url = try { Mobile.statusURL() } catch (e: Exception) { "" }
        if (url.isEmpty()) {
            startForeground(NOTIF_ID, buildNotification("网关未运行", "点按重新打开"))
            return
        }
        startForeground(NOTIF_ID, buildNotification("网关运行中", url))
    }

    private fun homeDir(): String = java.io.File(filesDir, "zen-gate").absolutePath

    private fun buildNotification(title: String, text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_stat_lamp)
            .setContentIntent(open)
            .setOngoing(true)
            .setSilent(true)
            .build()
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = getSystemService(NotificationManager::class.java)
        val ch = NotificationChannel(
            CHANNEL_ID, "Lamp Guide 网关", NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "网关运行状态（常驻通知，替代桌面托盘）"
            setShowBadge(false)
        }
        nm.createNotificationChannel(ch)
    }

    override fun onDestroy() {
        try { Mobile.stop() } catch (e: Exception) { /* 已经在停 */ }
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        const val CHANNEL_ID = "lamp-gateway"
        const val NOTIF_ID = 1101
    }
}
