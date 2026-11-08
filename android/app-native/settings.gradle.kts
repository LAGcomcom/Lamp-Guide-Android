// app-native 是独立可构建的 Gradle 工程：不动旧 app/ 的界面代码，
// 两者共用同一份 android/app/libs/lampguide.aar（gomobile 产物）。
pluginManagement {
    repositories {
        // 腾讯云镜像优先：这条线路上直连 google()/mavenCentral() 是几十 KB/s。
        maven { url = uri("https://mirrors.cloud.tencent.com/nexus/repository/maven-public/") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven { url = uri("https://mirrors.cloud.tencent.com/nexus/repository/maven-public/") }
        google()
        mavenCentral()
    }
}
rootProject.name = "lamp-guide-native"
include(":app")
