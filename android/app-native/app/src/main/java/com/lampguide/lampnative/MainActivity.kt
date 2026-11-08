package com.lampguide.lampnative

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.ViewModelProvider
import com.lampguide.lampnative.data.LogEntry
import com.lampguide.lampnative.data.LogView
import com.lampguide.lampnative.data.AdminClient
import com.lampguide.lampnative.data.UiError
import com.lampguide.lampnative.service.GatewayService
import com.lampguide.lampnative.ui.AppActions
import com.lampguide.lampnative.ui.AppViewModel
import com.lampguide.lampnative.ui.LampApp
import com.lampguide.lampnative.ui.Load
import com.lampguide.lampnative.ui.theme.LampTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * MainActivity 是原生界面：Compose Material3，**不加载 web/ 里任何一个文件**。
 *
 * 它只做界面之外那几件系统能力：剪贴板、深链、SAF、电池豁免、重启服务。
 * 数据仍只有一条路：mobile.Mobile.admin()（见 AdminClient）。
 */
class MainActivity : ComponentActivity(), AppActions {

    @Volatile
    override var errors: Load<List<String>> = Load.Loading

    private val errorsState = mutableStateOf<Load<List<String>>>(Load.Loading)
    private val keyDriftState = mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // 状态栏图标的明暗由**我们的调色板**决定，不是系统设置：应用自己按
        // LampTheme 跟随系统画明暗，而系统栏图标如果各自读系统值，浅色主题下
        // 会出现白字压浅底（真机实测看不见时间）。两处都钉在同一个判据上。
        // Activity 里拿不到 @Composable 的 isSystemInDarkTheme()，读同一份配置位。
        val dark = (resources.configuration.uiMode and
            android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
            android.content.res.Configuration.UI_MODE_NIGHT_YES
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.auto(
                android.graphics.Color.TRANSPARENT,
                android.graphics.Color.TRANSPARENT,
            ) { dark },
            navigationBarStyle = SystemBarStyle.auto(
                android.graphics.Color.TRANSPARENT,
                android.graphics.Color.TRANSPARENT,
            ) { dark },
        )

        val vm = ViewModelProvider(this)[AppViewModel::class.java]

        PeerReport.report(this)
        GatewayService.start(this)
        observeGatewayBoot(vm)
        observeKeyDrift(vm)

        setContent {
            LampTheme {
                LampApp(
                    vm = vm,
                    onStartGateway = {
                        GatewayService.start(this)
                        vm.notify("正在启动网关…")
                        scope.launch {
                            delayRefresh(vm)
                        }
                    },
                    actions = this,
                )
            }
        }
    }

    private val scope = CoroutineScope(Dispatchers.Main)

    private fun delayRefresh(vm: AppViewModel) {
        scope.launch {
            kotlinx.coroutines.delay(600)
            vm.loadState(true)
        }
    }

    /**
     * §6.2：服务侧已经在重试过了。这里只负责把「启动失败」的真实原因抬到首屏
     * ——服务把状态记在 companion 里，页面重启也能读到。
     */
    private fun observeGatewayBoot(vm: AppViewModel) {
        val (state, url, failure) = GatewayService.snapshot()
        if (state == GatewayService.State.FAILED && failure.isNotBlank()) {
            vm.notify(failure)
        }
    }

    /**
     * §6.8 重装即清 Key：本地只记最后一次给出去的 Key 的 hash（DataStore），
     * 与 `state.agentKeys["dsh-go"]` 比对，不同就把黄条抬出来。
     */
    private fun observeKeyDrift(vm: AppViewModel) {
        scope.launch {
            var last = -1L
            while (true) {
                val cur = (vm.state.value as? Load.Data)?.value
                val key = cur?.agentKeys?.get("dsh-go").orEmpty()
                if (cur != null && key.isNotBlank() && last < cur.uptimeSec) {
                    last = cur.uptimeSec
                    val fp = com.lampguide.lampnative.data.Mappers.keyFingerprint(key)
                    val stored = prefs().getString(AppViewModel.FINGERPRINT_KEY, null)
                    keyDriftState.value = stored != null && stored != fp
                    if (stored == null) prefs().edit().putString(AppViewModel.FINGERPRINT_KEY, fp).apply()
                }
                kotlinx.coroutines.delay(3000)
            }
        }
    }

    private fun prefs() = getSharedPreferences("lamp-native", Context.MODE_PRIVATE)

    // ------------------------------------------------------------- AppActions

    override fun keyDrifted(): Boolean = keyDriftState.value

    override fun copy(label: String, text: String): String {
        val cm = getSystemService(ClipboardManager::class.java)
        cm.setPrimaryClip(ClipData.newPlainText(label, text))
        // Android 13+ 系统自己会弹一个剪贴板提示，再 Toast 一次是噪声。
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            Toast.makeText(this, "$label 已复制", Toast.LENGTH_SHORT).show()
        }
        return "$label 已复制"
    }

    /**
     * §6.3 深链：原生侧自己做白名单式 scheme 校验（http/https 或 xxx://），
     * 空 scheme 与 intent: 拒绝；ActivityNotFoundException 如实提示。
     */
    override fun openDshGo() {
        val intent = Intent(Intent.ACTION_MAIN).apply {
            setPackage(PeerReport.DSH_GO)
            addCategory(Intent.CATEGORY_LAUNCHER)
        }
        try {
            startActivity(intent)
            return
        } catch (e: Exception) {
            // 包不在（或不可见）就退回 scheme 深链。
        }
        val uri = Uri.parse("dshgo://open?page=models")
        if (!schemeAllowed(uri)) {
            Toast.makeText(this, "这个链接的 scheme 不被允许：${uri.scheme}", Toast.LENGTH_LONG).show()
            return
        }
        try {
            startActivity(Intent(Intent.ACTION_VIEW, uri))
        } catch (e: android.content.ActivityNotFoundException) {
            Toast.makeText(this, "没找到能打开这个链接的应用", Toast.LENGTH_LONG).show()
        } catch (e: Exception) {
            Toast.makeText(this, "打开失败：${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    /**
     * 打开授权页：登录弹窗里那个「打开授权页面」按的就是这里。
     *
     * 走 ACTION_VIEW 交给系统浏览器 —— 应用内 WebView 登录会在 OAuth 的
     * cookie 域上与系统浏览器分家，用户在那一边登完回来还是未登录状态。
     * 打不开就如实回一句原因，不静默。
     */
    override fun openUrl(url: String): String {
        val uri = Uri.parse(url)
        if (!schemeAllowed(uri)) {
            return "这个链接的 scheme 不被允许：" + (uri.scheme ?: "（空）")
        }
        return try {
            startActivity(Intent(Intent.ACTION_VIEW, uri).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            "已在浏览器打开授权页面"
        } catch (e: android.content.ActivityNotFoundException) {
            "没找到能打开这个链接的应用"
        } catch (e: Exception) {
            "打开失败：" + (e.message ?: e.toString())
        }
    }

    private fun schemeAllowed(uri: Uri): Boolean {
        val s = uri.scheme?.lowercase() ?: return false
        if (s == "intent") return false
        return s == "http" || s == "https" || s.endsWith("://") || s.matches(Regex("[a-z][a-z0-9+.-]*"))
    }

    override fun restartGateway() {
        GatewayService.restart(this)
    }

    /**
     * §6.6 电池优化：ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS。
     * 用户拒绝就只显示提示，不纠缠。
     */
    override fun requestBatteryExemption() {
        val pm = getSystemService(PowerManager::class.java)
        if (pm.isIgnoringBatteryOptimizations(packageName)) {
            Toast.makeText(this, "已经在后台白名单里了", Toast.LENGTH_SHORT).show()
            return
        }
        try {
            startActivity(
                Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
                    .setData(Uri.parse("package:$packageName")),
            )
        } catch (e: Exception) {
            try {
                startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
            } catch (e2: Exception) {
                Toast.makeText(this, "这台机器没有电池优化设置页", Toast.LENGTH_SHORT).show()
            }
        }
    }

    /**
     * 日志面只保留「最近 20 条错误」：固定 `level=error&limit=20`，
     * 不暴露七类日志文件的浏览器。
     */
    override fun loadErrors() {
        scope.launch {
            val client = AdminClient()
            when (val r = client.get<LogView>("${AdminClient.Admin.LOGS}?level=error&limit=20")) {
                is AdminClient.Result.Ok -> {
                    val lines = r.value.entries.map { e ->
                        "${e.at} ${e.level}${if (e.cat.isBlank()) "" else " [${e.cat}]"} ${e.msg}"
                    }
                    errorsState.value = if (lines.isEmpty()) Load.Empty else Load.Data(lines)
                }
                is AdminClient.Result.Err ->
                    errorsState.value = Load.Error(UiError(r.code, r.message))
            }
            errors = errorsState.value
        }
    }

    override fun onDestroy() {
        // 网关在 Service 里跑：Activity 走了它继续活着（这是安卓对托盘的等价物）。
        super.onDestroy()
    }
}
