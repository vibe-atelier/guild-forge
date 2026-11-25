# Builds Nexus into the "dist" folder:
#   nexus           the desktop app (Windows here; on Linux use build.sh)
#   guild-forge     the guild host's commands (guild-forge-cmd.exe on Windows)
#   GuildForge      the guild host's window (GuildForge.exe, Windows only)
#   nexus-devclient the small test client (Windows and Linux)
# Run from this folder in PowerShell:   .\build.ps1
#
# The app's window is drawn with Fyne, which needs a C compiler: install
# MSYS2 (msys2.org), run "pacman -S mingw-w64-x86_64-toolchain" in
# "MSYS2 MinGW x64", and add C:\msys64\mingw64\bin to PATH. The Linux app is
# built on Linux itself (build.sh), because its C part can't be built here.
$ErrorActionPreference = 'Stop'

$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }
if (-not (Test-Path $go)) { throw 'Go was not found. Install it from https://go.dev/dl' }

function Build($os, $cgo, $program, $out, $flags, $tags = "") {
    $env:GOOS = $os
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = $cgo
    $goArgs = @("build", "-trimpath")
    if ($tags) { $goArgs += @("-tags", $tags) }  # an empty argument would be lost
    & $go @goArgs -ldflags $flags -o $out "./cmd/$program"
    if ($LASTEXITCODE -ne 0) {
        $name = [IO.Path]::GetFileNameWithoutExtension($out)
        if ($os -eq 'windows' -and (Get-Process $name -ErrorAction SilentlyContinue)) {
            throw "Could not replace $out because $name is running. Close it and build again."
        }
        throw "Build failed: $program for $os"
    }
    Write-Host "Built $out"
}

Push-Location $PSScriptRoot
try {
    # Strong noise suppression: RNNoise's model as a weights file,
    # made from Xiph's model (rnnoise_data-*.tar.gz unpacked into the
    # rnnoise folder) when it isn't made yet.
    $model = "rnnoise-v0.2\rnnoise-v0.2\src\rnnoise_data.c"
    if (-not (Test-Path "models\rnnoise.bin") -and (Test-Path $model)) {
        New-Item -ItemType Directory -Force "models" | Out-Null
        & $go run ./tools/rnnoise-weights $model "models\rnnoise.bin"
    }
    # The emoji font and the noise model are built into the programs,
    # which write them into the PC's nexus-data when they start. The
    # files are put where the assets package builds them in; whatever is
    # missing is simply left out.
    $assets = "internal\assets\files"
    New-Item -ItemType Directory -Force $assets | Out-Null
    $appTags = @()
    $font = @("fonts\NotoColorEmoji.ttf", "NotoColorEmoji.ttf") | Where-Object { Test-Path $_ } | Select-Object -First 1
    if ($font) {
        Copy-Item $font "$assets\NotoColorEmoji.ttf" -Force
        Copy-Item "fonts\OFL.txt" "$assets\OFL.txt" -Force
        $appTags += "assetfont"
    }
    if (Test-Path "models\rnnoise.bin") {
        Copy-Item "models\rnnoise.bin" "$assets\rnnoise.bin" -Force
        Copy-Item "internal\rnnoise\csrc\COPYING" "$assets\COPYING-RNNoise.txt" -Force
        $appTags += "assetmodel"
    }
    $forgeTags = ($appTags | Where-Object { $_ -eq "assetfont" }) -join ","
    $appTags = $appTags -join ","

    foreach ($os in 'windows', 'linux') {
        $ext = if ($os -eq 'windows') { '.exe' } else { '' }
        Build $os '0' 'nexus-devclient' "dist\$os-amd64\nexus-devclient$ext" '-s -w'
    }
    # guild-forge, the commands, is pure Go, so both systems' copies are
    # built here.
    # On Windows the file is guild-forge-cmd.exe; on Linux it is
    # installed as guild-forge.
    Build 'windows' '0' 'guild-forge-cmd' 'dist\windows-amd64\guild-forge-cmd.exe' '-s -w'
    Build 'linux' '0' 'guild-forge-cmd' 'dist\linux-amd64\guild-forge' '-s -w'
    if (Get-Command gcc -ErrorAction SilentlyContinue) {
        # GuildForge's window has no console, and carries the emoji font and
        # its own notices for its Licenses page.
        $env:GOOS = 'windows'; $env:CGO_ENABLED = '1'
        & $go run ./tools/notices -o 'cmd\guild-forge-window\NOTICES.txt' -goos windows -cgo 1 -tags $forgeTags -title GuildForge ./cmd/guild-forge-window
        if ($LASTEXITCODE -ne 0) { throw 'Could not write GuildForge''s notices.' }
        $windowTags = (@($forgeTags, 'notices') | Where-Object { $_ }) -join ','
        Build 'windows' '1' 'guild-forge-window' 'dist\windows-amd64\GuildForge.exe' '-s -w -H windowsgui' $windowTags
        # No console window for the app, which carries the font, the model and
        # its own notices for its Licenses pages.
        & $go run ./tools/notices -o 'cmd\nexus\NOTICES.txt' -goos windows -cgo 1 -tags $appTags -title Nexus ./cmd/nexus
        if ($LASTEXITCODE -ne 0) { throw 'Could not write the app''s notices.' }
        $nexusTags = (@($appTags, 'notices') | Where-Object { $_ }) -join ','
        Build 'windows' '1' 'nexus' 'dist\windows-amd64\nexus.exe' '-s -w -H windowsgui' $nexusTags
    } else {
        Write-Host "Skipped the app (nexus.exe) and GuildForge.exe: no C compiler (gcc) found. See the top of this file."
    }
    if ($appTags) { Write-Host "Built in: $appTags" }
    # The programs no longer need fonts and models folders beside them.
    foreach ($os in 'windows', 'linux') {
        foreach ($old in "fonts", "models") {
            Remove-Item -Recurse -Force "dist\$os-amd64\$old" -ErrorAction SilentlyContinue
        }
    }
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
