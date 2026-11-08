// 顶层构建文件。AGP 8.13.x 配 Gradle 8.14.3、JDK 17+；
// org.gradle.java.home 在 gradle.properties 里钉到 Temurin 21（系统默认 java 是 8，跑不动 AGP）。
plugins {
    id("com.android.application") version "8.13.2" apply false
    id("org.jetbrains.kotlin.android") version "2.2.20" apply false
}
