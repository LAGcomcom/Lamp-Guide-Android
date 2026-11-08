pluginManagement {
    repositories {
        // 腾讯云镜像优先：直连 google()/mavenCentral() 在这条线路上是几十 KB/s，
        // 镜像按字节一致。
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
rootProject.name = "lamp-guide-android"
include(":app")
