package com.lampguide.app

import android.content.Context
import android.content.pm.PackageManager
import mobile.Mobile
import org.json.JSONObject

/**
 * PeerReport 只回答一个问题：这台手机上装了哪些兄弟 App。
 *
 * 网关那边的 Agent 面要区分「这台机器上没有 DSH Go」和「没检测到配置文件」。
 * 后者它能自己查，前者只有宿主能查——一台独立 APK 的文件在应用私有存储里，
 * 网关（哪怕同进程）看不见它的 proot rootfs。所以这里按包名问一次
 * PackageManager，把答案交给 Go 侧记录（mobile.SetPeers）。
 *
 * 报告是纯事实，不做任何注入：网关不会去写另一台 App 的配置，被写的只有
 * 本机程序的配置文件（桌面那一批），而安卓上那批根本不存在。
 *
 * 启动路径有两条：Activity 的 onCreate（用户点图标进来）和前台服务的
 * onStartCommand（进程被回收后 START_STICKY 重建）。两条都要报，否则重建后
 * 页面会退化成「未上报」——一个本来能答的问题不该因为进程重启就变成未知。
 */
object PeerReport {

    /** DSH Go 的包名，与它自己 AndroidManifest 的 package 一致。 */
    private const val DSH_GO = "io.github.ccch1mneyyy.dsh_app"

    fun report(context: Context) {
        val pm = context.packageManager
        val dshGo = try {
            pm.getPackageInfo(DSH_GO, 0)
        } catch (e: PackageManager.NameNotFoundException) {
            null
        }
        val info = JSONObject().apply {
            put("dshGo", dshGo != null)
            if (dshGo != null) {
                put("dshGoVersion", dshGo.versionName ?: "")
                put("dshGoPackage", dshGo.packageName)
            }
        }
        try {
            Mobile.setPeers(info.toString())
        } catch (e: Exception) {
            /* 上报失败只让页面多说一句「未上报」，不该拦住启动 */
        }
    }
}
