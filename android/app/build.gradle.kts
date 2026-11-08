plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.lampguide.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.lampguide.app"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "1.0.0"
        // gomobile 生成的是 arm64 libgojni.so；只保留这一种 ABI，
        // 免得装到 x86 模拟器上得到一个「找不到 native 方法」的包。
        ndk { abiFilters += "arm64-v8a" }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // 先用 debug keystore 直装真机；分发前换成自己的 release key。
            signingConfig = signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }

    // 界面资源原样打进 APK，由 WebViewAssetLoader 从 https://appassets.androidplatform.net/
    // 提供：这样 ES 模块、modulepreload、fetch 都在一个真正的 https 源上，
    // 与桌面版 WebView2 的虚拟 https 主机是同一套前提。
    sourceSets["main"].assets.srcDirs("src/main/assets")

    packaging {
        jniLibs { useLegacyPackaging = false }
    }
}

dependencies {
    implementation(files("libs/lampguide.aar"))
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.appcompat:appcompat:1.7.1")
    implementation("androidx.webkit:webkit:1.12.1")
}
