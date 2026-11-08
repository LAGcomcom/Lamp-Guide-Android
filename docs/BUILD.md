# 构建安卓客户端

## 环境

| 工具 | 工程使用的版本 |
| --- | --- |
| JDK | 21 |
| Go | 1.26.0，以 `gateway/go.mod` 为准 |
| Android SDK | platform android-36、build-tools 36.0.0 |
| Android NDK | 27.2.12479018 |
| Gradle | Wrapper 8.14.3 |
| Android Gradle Plugin | 8.13.2 |
| Kotlin | 2.2.20 |

设置 `JAVA_HOME`、`ANDROID_HOME`（也可用 `ANDROID_SDK_ROOT`）和 `ANDROID_NDK_HOME`。路径取决于你的机器，仓库不固定盘符。Gradle 仓库保留腾讯镜像优先、Google 和 Maven Central 兜底的顺序。

## 构建

```powershell
# 生成 AAR 并构建原生 release APK
./scripts/build-android.ps1 -BuildType Release

# debug APK
./scripts/build-android.ps1 -BuildType Debug

# Go 代码未改且 AAR 已生成时，可跳过绑定
./scripts/build-android.ps1 -BuildType Release -SkipBind
```

`scripts/bind-aar.ps1` 根据 `go.mod` 安装对应版本的 gomobile/gobind，并使用：

```text
gomobile bind -target=android/arm64 -androidapi 24 -ldflags=-extldflags=-Wl,-z,max-page-size=16384 -o android/app/libs/lampguide.aar ./mobile
```

实际执行目录是 `gateway/`，输出路径由脚本转换成绝对路径。链接参数用于 Go 共享库的 16 KB 页对齐。首次运行会下载 Go、Android 和 Gradle 依赖。

原生工程使用同一份 `android/app/libs/lampguide.aar`。构建入口明确指向 `android/app-native/`，避免误构建旧 WebView 工程。

## 验证

```powershell
Push-Location gateway
go test ./internal/zen/... ./internal/host/...
Pop-Location

./android/app-native/gradlew.bat --project-dir android/app-native testDebugUnitTest
```

生成 AAR 后可在 Android Studio 打开 `android/app-native/`。用 Android SDK 的 `apksigner verify` 检查签名，`zipalign -c -P 16 4` 检查 APK 对齐；检查 ELF 页对齐需另外解包 AAR 并使用 NDK 的 `llvm-readelf -l`。

## 签名和升级

当前 `release` 构建配置引用本机 debug keystore，便于测试安装，不提供任何 keystore 文件。公开预览 APK 来自发布工作站的现有开发签名；在另一台机器重建时签名可能不同。

长期分发前，应自行配置并妥善保存正式签名。更换签名会阻止直接覆盖旧版本；卸载应用会删除应用私有数据。导出备份需按应用支持的流程操作。

## 图标

安装 Node.js 后在仓库根目录运行：

```powershell
npm ci
npm run icons
```

渲染器版本固定为 `@resvg/resvg-js 2.6.2`。SVG、Android 前景/背景/单色资源和通知栏图标来自同一组几何。
