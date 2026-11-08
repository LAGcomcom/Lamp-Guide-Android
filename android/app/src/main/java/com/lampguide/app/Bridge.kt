package com.lampguide.app

import android.app.Activity
import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import android.webkit.WebView
import mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

/**
 * Bridge 是页面与 Go 宿主之间唯一的通道，协议与桌面版 internal/shell 的
 * Bridge 逐字一致：
 *
 *   页面 → 宿主：window.chrome.webview.postMessage(JSON.stringify({id, method, payload}))
 *   宿主 → 页面：window.__hostReceive(JSON.stringify({id, ok, data, error}))
 *
 * 所以页面一行都不用改。区别只有两处：
 *
 * 1. 桌面的四个「本机动作」（PickSavePath/PickOpenPath/OpenPath/OpenUrl）
 *    在安卓上没有 Win32/COM 可用，改由这里用 SAF 与系统 Intent 回答；
 * 2. 窗口类方法（Window.*）没有原生窗口可操作，回 ok:true 而不是报错——
 *    页面对它们的返回本来就不读。
 *
 * 网关调用一律投到工作线程：mobile.Mobile.admin 是一次真实的闸道往返，
 * 在 JS 桥线程上同步做会把整个页面的其它调用堵住。
 */
class Bridge(private val activity: Activity, private val web: WebView) {

    private val worker = Executors.newSingleThreadExecutor { r ->
        Thread(r, "lamp-gateway-bridge").apply { isDaemon = true }
    }
    private val seq = AtomicInteger(0)

    /** 保存类导出：先把字节写进缓存目录，用户选了目标之后再搬过去。 */
    private val pendingSaves = mutableMapOf<String, Uri>()

    // ---------------------------------------------------------------- 页面入口

    @android.webkit.JavascriptInterface
    fun post(raw: String) {
        val msg = try {
            JSONObject(raw)
        } catch (e: Exception) {
            return
        }
        val id = msg.optInt("id", -1)
        val method = msg.optString("method", "")
        val payload = msg.optJSONObject("payload") ?: JSONObject()

        when (method) {
            // 窗口控制：安卓没有可操作的原生窗框
            "Window.Minimize", "Window.ToggleMaximize", "Window.Close",
            "Window.StartDrag", "Window.StartResize", "Window.SetHitRegions" ->
                reply(id, true, null, null)

            "Window.IsMaximized" -> reply(id, true, JSONObject().put("isMaximized", false), null)
            "Window.State" -> reply(id, true, statePayload(), null)

            "DIAG" -> reply(id, true, null, null)

            "PickOpenPath" -> activity.runOnUiThread { askOpen(id) }
            "PickSavePath" -> activity.runOnUiThread { askSave(id, payload) }
            "OpenPath" -> activity.runOnUiThread { openPath(id, payload) }
            "OpenUrl" -> activity.runOnUiThread { openUrl(id, payload) }

            "Gateway" -> worker.execute { callGateway(id, payload) }

            else -> reply(id, false, null, "未知的方法：" + method)
        }
    }

    /** 宿主主动推送（没有 id）：网关就绪、服务状态变化。 */
    fun push(type: String, data: JSONObject?) {
        val msg = JSONObject().put("type", type)
        if (data != null) msg.put("data", data)
        deliver(msg)
    }

    // ---------------------------------------------------------------- 网关

    private fun callGateway(id: Int, payload: JSONObject) {
        val path = payload.optString("path", "")
        if (path.isEmpty()) {
            reply(id, false, null, "缺少 path")
            return
        }
        // body 原样以 JSON 文本交给 Go：那边只解一次，和桌面桥一致。
        val bodyJson = payload.opt("body")?.let { if (it == JSONObject.NULL) "" else it.toString() } ?: ""
        val saveTo = payload.optString("saveTo", "")
        val loadFrom = payload.optString("loadFrom", "")

        val raw = try {
            Mobile.admin(path, bodyJson, saveTo, loadFrom)
        } catch (e: Exception) {
            reply(id, false, null, "网关调用失败：" + (e.message ?: e.toString()))
            return
        }

        val env = try { JSONObject(raw) } catch (e: Exception) {
            reply(id, false, null, "宿主回复不是合法 JSON")
            return
        }
        val ok = env.optBoolean("ok", false)
        val data = if (env.has("data")) env.opt("data") else null
        val error = if (env.has("error")) env.optString("error") else null
        val code = if (env.has("code")) env.optString("code") else null

        // 导出：网关把字节写到了缓存路径，这里再搬到用户选的那个文档。
        if (ok && saveTo.isNotEmpty()) {
            val target = pendingSaves.remove(saveTo)
            if (target != null) {
                val moved = copyInto(target, File(saveTo))
                if (!moved) {
                    reply(id, false, code, "写入所选文件失败：" + target)
                    return
                }
            }
        }
        reply(id, ok, data, if (ok) null else (error ?: "网关返回了失败"))
    }

    // ---------------------------------------------------------------- 本机动作

    private fun askOpen(id: Int) {
        currentPickId = id
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*"
        }
        activity.startActivityForResult(intent, REQ_OPEN)
    }

    private fun askSave(id: Int, payload: JSONObject) {
        currentPickId = id
        currentSaveName = payload.optString("name", "untitled").ifEmpty { "untitled" }
        val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "application/json"
            putExtra(Intent.EXTRA_TITLE, currentSaveName)
        }
        activity.startActivityForResult(intent, REQ_SAVE)
    }

    private fun openPath(id: Int, payload: JSONObject) {
        val path = payload.optString("path", "").trim()
        if (path.isEmpty()) { reply(id, false, null, "缺少 path"); return }
        val target = File(path)
        if (!target.exists()) { reply(id, false, null, "找不到这个位置：" + path); return }
        // 安卓没有资源管理器：交给系统把它当成目录打开（多数文件管理器会接住）
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(Uri.fromFile(target), "resource/folder")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        val done = try { activity.startActivity(intent); true } catch (e: Exception) { false }
        if (!done) {
            reply(id, false, null, "这台设备没有能打开目录的应用；数据目录是 " + path)
            return
        }
        reply(id, true, JSONObject().put("path", path), null)
    }

    private fun openUrl(id: Int, payload: JSONObject) {
        val url = payload.optString("url", "").trim()
        val lower = url.lowercase()
        /* http(s) 交给浏览器；其它 scheme 交给能接它的那个应用。
           后一条是这次真机测出来的：Agent 面上的「打开 DSH Go 的模型页」用的是
           dshgo://，被只认 http 的这道门直接打回，页面只好画一条红条。
           scheme 白名单而不是放行任何字符串：一个空 scheme 或 intent: 会把
           任意组件拉起来，那是页面给不出、也不该给的能力。 */
        val schemeOk = lower.startsWith("http://") || lower.startsWith("https://") ||
            Regex("^[a-z][a-z0-9+.-]*://").containsMatchIn(lower)
        if (!schemeOk) {
            reply(id, false, null, "地址缺少协议头")
            return
        }
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
        val done = try { activity.startActivity(intent); true } catch (e: Exception) { false }
        if (!done) { reply(id, false, null, "没有能打开链接的应用"); return }
        reply(id, true, JSONObject().put("url", url), null)
    }

    /** Activity result 回到这里；Bridge 由 MainActivity 转发。 */
    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        val id = currentPickId
        currentPickId = -1
        if (id < 0) return false

        if (resultCode != Activity.RESULT_OK || data?.data == null) {
            // 用户取消是**正常回答**，不是失败：页面靠 canceled 优雅退出。
            reply(id, true, JSONObject().put("canceled", true).put("path", ""), null)
            return true
        }
        val uri = data.data!!

        when (requestCode) {
            REQ_OPEN -> worker.execute {
                val copied = try { importToCache(uri) } catch (e: Exception) { null }
                if (copied == null) {
                    reply(id, false, null, "读取所选文件失败")
                } else {
                    reply(id, true, JSONObject().put("canceled", false).put("path", copied.absolutePath), null)
                }
            }

            REQ_SAVE -> {
                // 先给网关一个真实路径；等它写完，callGateway 再搬到这个 uri。
                val name = currentSaveName.ifEmpty { "untitled" }
                val staging = File(exportDir(), sanitize(name))
                staging.parentFile?.mkdirs()
                pendingSaves[staging.absolutePath] = uri
                reply(id, true, JSONObject().put("canceled", false).put("path", staging.absolutePath), null)
            }

            else -> reply(id, false, null, "未知的选择结果")
        }
        return true
    }

    // ---------------------------------------------------------------- 小件

    private fun importToCache(uri: Uri): File? {
        val resolver: ContentResolver = activity.contentResolver
        val name = displayName(uri) ?: ("import-" + System.currentTimeMillis() + ".json")
        val target = File(importDir(), sanitize(name))
        target.parentFile?.mkdirs()
        resolver.openInputStream(uri)?.use { input ->
            target.outputStream().use { out -> input.copyTo(out) }
        } ?: return null
        return target
    }

    private fun copyInto(uri: Uri, source: File): Boolean = try {
        activity.contentResolver.openOutputStream(uri)?.use { out ->
            source.inputStream().use { it.copyTo(out) }
        }
        true
    } catch (e: Exception) {
        false
    }

    private fun displayName(uri: Uri): String? {
        return try {
            activity.contentResolver.query(uri, null, null, null, null)?.use { c ->
                val idx = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (idx >= 0 && c.moveToFirst()) c.getString(idx) else null
            }
        } catch (e: Exception) {
            null
        }
    }

    private fun sanitize(name: String): String {
        val cleaned = name.replace(Regex("[\\\\/:*?\"<>|]"), "_").trim()
        return cleaned.ifEmpty { "untitled" }.take(120)
    }

    private fun importDir(): File = File(activity.filesDir, "imports").apply { mkdirs() }

    private fun exportDir(): File = File(activity.cacheDir, "exports").apply { mkdirs() }

    private fun statePayload(): JSONObject = JSONObject()
        .put("isMaximized", false)
        .put("scale", activity.resources.displayMetrics.density.toDouble())
        .put("clientWidth", web.width)
        .put("clientHeight", web.height)

    private fun reply(id: Int, ok: Boolean, data: Any?, error: String?) {
        val msg = JSONObject().put("id", id).put("ok", ok)
        if (data != null) msg.put("data", data)
        if (error != null) msg.put("error", error)
        deliver(msg)
    }

    private fun deliver(msg: JSONObject) {
        // 引号转义必须由 JSONObject 做：错误消息里有中文与路径分隔符。
        val literal = JSONObject.quote(msg.toString())
        activity.runOnUiThread {
            web.evaluateJavascript("window.__hostReceive && window.__hostReceive($literal)", null)
        }
    }

    companion object {
        const val REQ_OPEN = 1001
        const val REQ_SAVE = 1002
        @Volatile private var currentPickId = -1
        @Volatile private var currentSaveName = "untitled"
    }
}
