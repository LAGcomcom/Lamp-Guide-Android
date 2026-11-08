param()
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = $env:ANDROID_SDK_ROOT }
if (-not $env:ANDROID_HOME) { throw 'Set ANDROID_HOME or ANDROID_SDK_ROOT to your Android SDK.' }
if (-not $env:ANDROID_NDK_HOME) { throw 'Set ANDROID_NDK_HOME to Android NDK 27.2.' }
if (-not $env:JAVA_HOME) { throw 'Set JAVA_HOME to JDK 21.' }
$env:PATH = (Join-Path $env:JAVA_HOME 'bin') + [IO.Path]::PathSeparator + $env:PATH
Push-Location (Join-Path $projectRoot 'gateway')
try {
    $mobileVersion = (& go list -m -f '{{.Version}}' golang.org/x/mobile).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Unable to resolve pinned x/mobile version.' }
    & go install "golang.org/x/mobile/cmd/gomobile@$mobileVersion"
    if ($LASTEXITCODE -ne 0) { throw 'gomobile installation failed.' }
    & go install "golang.org/x/mobile/cmd/gobind@$mobileVersion"
    if ($LASTEXITCODE -ne 0) { throw 'gobind installation failed.' }
    $goBin = ([string](& go env GOBIN)).Trim()
    if (-not $goBin) { $goBin = Join-Path ((& go env GOPATH).Trim().Split([IO.Path]::PathSeparator)[0]) 'bin' }
    $env:PATH = $goBin + [IO.Path]::PathSeparator + $env:PATH
    $outputDir = Join-Path $projectRoot 'android/app/libs'
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
    & (Join-Path $goBin 'gomobile') bind -target=android/arm64 -androidapi 24 '-ldflags=-extldflags=-Wl,-z,max-page-size=16384' -o (Join-Path $outputDir 'lampguide.aar') ./mobile
    if ($LASTEXITCODE -ne 0) { throw "gomobile bind failed: $LASTEXITCODE" }
} finally { Pop-Location }
