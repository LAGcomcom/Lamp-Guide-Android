param(
    [ValidateSet('Debug', 'Release')] [string]$BuildType = 'Release',
    [switch]$SkipBind,
    [string]$GradleExecutable
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = $env:ANDROID_SDK_ROOT }
if (-not $env:ANDROID_HOME) { throw 'Set ANDROID_HOME or ANDROID_SDK_ROOT.' }
if (-not $env:JAVA_HOME) { throw 'Set JAVA_HOME to JDK 21.' }
$env:PATH = (Join-Path $env:JAVA_HOME 'bin') + [IO.Path]::PathSeparator + $env:PATH
if (-not $SkipBind) { & (Join-Path $PSScriptRoot 'bind-aar.ps1') }
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot 'android/app/libs/lampguide.aar'))) {
    throw 'Missing AAR: run scripts/bind-aar.ps1 or omit -SkipBind.'
}
$nativeProject = Join-Path $projectRoot 'android/app-native'
if (-not $GradleExecutable) {
    $GradleExecutable = Join-Path $nativeProject $(if ($IsLinux -or $IsMacOS) { 'gradlew' } else { 'gradlew.bat' })
}
& $GradleExecutable --project-dir $nativeProject --no-daemon --console=plain "assemble$BuildType"
if ($LASTEXITCODE -ne 0) { throw "Gradle build failed: $LASTEXITCODE" }
$variant = $BuildType.ToLowerInvariant()
$apk = Join-Path $nativeProject "app/build/outputs/apk/$variant/app-$variant.apk"
$dist = Join-Path $projectRoot 'dist'
New-Item -ItemType Directory -Path $dist -Force | Out-Null
$target = Join-Path $dist "lamp-guide-native-1.0.0-$variant-arm64.apk"
Copy-Item -LiteralPath $apk -Destination $target -Force
Get-FileHash -LiteralPath $target -Algorithm SHA256
