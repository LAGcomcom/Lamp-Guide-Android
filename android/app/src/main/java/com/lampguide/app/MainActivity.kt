package com.lampguide.app

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.ViewGroup
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.webkit.WebViewAssetLoader
import mobile.Mobile
import org.json.JSONObject

/**
 * MainActivity 只做两件事：把页面装进 WebView，回答页面的「本机动作」。
 *
 * 界面来自 APK 里的 assets/ui，由 WebViewAssetLoader 挂在
 * https://appassets.androidplatform.net/ 上——和桌面版 WebView2 用虚拟 https
 * 主机是同一套前提：ES 模块、modulepreload 与 fetch 都需要一个真正的安全源，
 * file:// 与 data: 都不满足。
 *
 * 页面的数据仍然只有一条路：window.chrome.webview.postMessage →
 * [Bridge] → mobile.Mobile.admin() → 网关真实的管理路由。这里不放宽任何
 * Origin 校验，也不复制任何业务逻辑。
 */
class MainActivity : AppCompatActivity() {

    private lateinit var web: WebView
    private lateinit var bridge: Bridge
    private lateinit var loader: WebViewAssetLoader

    private val askNotification =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            /* 拒绝不影响网关：常驻通知只是它的可视面 */
        }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        loader = WebViewAssetLoader.Builder()
            .setDomain(ASSET_HOST)
            // 前缀 "/" 是对的：loader 会把前缀剥掉再交给 handler，注册 "/ui/" 会
            // 让 /ui/js/shell.js 被解析成 assets/js/shell.js（少一层 ui）。
            .addPathHandler("/", WebViewAssetLoader.AssetsPathHandler(this))
            .build()

        web = WebView(this)
        web.setBackgroundColor(Color.TRANSPARENT)
        web.layoutParams = ViewGroup.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT
        )
        setContentView(web)

        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            mediaPlaybackRequiresUserGesture = false
            cacheMode = WebSettings.LOAD_DEFAULT
            // 页面语言是中文，注释与日志都不该走系统的默认编码猜测
            mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            web.settings.safeBrowsingEnabled = false
        }

        bridge = Bridge(this, web)
        web.addJavascriptInterface(bridge, "LampAndroid")

        /* 控制台转 logcat：页面的任何异常现在只在 WebView 里，抓不到就只能靠
           一张截图猜。debug 构建同时还开 WebView 调试，愿意的话能挂 devtools。 */
        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true)
        }
        /* 控制台在 WebChromeClient 上，不在 WebViewClient 上——写错那一侧会
           得到一个「overrides nothing」的编译错误，而页面的异常依旧无声。 */
        web.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(msg: ConsoleMessage): Boolean {
                val line = "[web] " + msg.messageLevel() + " " + msg.message() +
                    "  @" + msg.sourceId() + ":" + msg.lineNumber()
                if (msg.messageLevel() == ConsoleMessage.MessageLevel.ERROR) {
                    Log.e("LampWeb", line)
                } else {
                    Log.i("LampWeb", line)
                }
                return true
            }
        }

        web.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(
                view: WebView, request: WebResourceRequest
            ): WebResourceResponse? = loader.shouldInterceptRequest(request.url)

            override fun shouldOverrideUrlLoading(
                view: WebView, request: WebResourceRequest
            ): Boolean {
                // 页面里的外链（渠道授权页、发布页）交给系统浏览器：那是用户
                // 真的要离开这个应用的地方，在 WebView 里打开会没有地址栏。
                val url = request.url
                if (url.host != ASSET_HOST) {
                    try {
                        startActivity(Intent(Intent.ACTION_VIEW, url))
                    } catch (e: Exception) {
                        /* 没有浏览器就留在页面里，不静默跳走 */
                        return false
                    }
                    return true
                }
                return false
            }
        }

        PeerReport.report(this)
        applyInsets()
        startGateway()
        web.loadUrl("https://$ASSET_HOST/ui/index.html")

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            askNotification.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    /**
     * 系统栏与刘海：把实际的 inset 写进 CSS 变量，页面才知道该让多少。
     * WebView 在 Android 上不吃 env(safe-area-inset-*)，所以由这里量出来喂给
     * mobile.css 里的 --lg-* 变量。
     */
    private fun applyInsets() {
        ViewCompat.setOnApplyWindowInsetsListener(web) { view, insets ->
            val bars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
            )
            val d = resources.displayMetrics.density
            val js = buildString {
                append("(function(){var s=document.documentElement.style;")
                append("s.setProperty('--lg-status-h','").append(dp(bars.top, d)).append("px');")
                append("s.setProperty('--lg-inset-top','").append(dp(bars.top, d)).append("px');")
                append("s.setProperty('--lg-inset-bottom','").append(dp(bars.bottom, d)).append("px');")
                append("s.setProperty('--lg-inset-left','").append(dp(bars.left, d)).append("px');")
                append("s.setProperty('--lg-inset-right','").append(dp(bars.right, d)).append("px');")
                append("})();")
            }
            view.post { web.evaluateJavascript(js, null) }
            insets
        }
        ViewCompat.requestApplyInsets(web)
    }

    private fun dp(px: Int, density: Float): String =
        String.format(java.util.Locale.US, "%.1f", px / density)

    /**
     * 网关在独立的 Service 里跑：Activity 被系统回收时它继续活着，回到前台
     * 重建这个窗口就足够——这是安卓对「托盘常驻」的等价物。
     */
    private fun startGateway() {
        try {
            ContextCompat.startForegroundService(this, Intent(this, GatewayService::class.java))
        } catch (e: Exception) {
            bridge.push("gateway-error", JSONObject().put("error", e.message ?: "服务启动失败"))
        }
    }

    /** SAF 的选择结果回到 Bridge：它自己知道是哪个请求、要回答什么。 */
    @Deprecated("startActivityForResult 仍然是最短的 SAF 路径，避免引入 Fragment 依赖")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (bridge.onActivityResult(requestCode, resultCode, data)) {
            super.onActivityResult(requestCode, resultCode, data)
            return
        }
        super.onActivityResult(requestCode, resultCode, data)
    }

    /** 横竖屏切换后抽屉/并排的判定要跟着变；mobile.js 自己也监听，这里只补一次。 */
    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        web.evaluateJavascript(
            "(function(){var h=document.documentElement;" +
                "var m=window.innerWidth<900?'drawer':'wide';" +
                "h.setAttribute('data-lg-mode',m);" +
                "if(m==='wide')h.setAttribute('data-lg-drawer','closed');})();",
            null
        )
    }

    override fun onDestroy() {
        web.removeJavascriptInterface("LampAndroid")
        web.destroy()
        super.onDestroy()
    }

    companion object {
        const val ASSET_HOST = "appassets.androidplatform.net"
        const val PORT = 8787
    }
}
