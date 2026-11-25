# Makes the files for a GitHub release in release\v<VERSION>\:
# nexus_chat-client_Windows.exe (the app), GuildForge-Windows_GUI.exe,
# guild-forge_Windows-cmd.exe and guild-forge_Debian,
# Third-Party_Notices.txt with the licence of everything built into
# them (written by tools\notices), and SHA256SUMS.txt, each file's fingerprint
# for checking downloads. It uploads nothing; the release is made on GitHub
# by hand. Run .\build.ps1 first, then, from the project folder:
#   .\tools\release.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
Push-Location $root
try {
    $go = (Get-Command go -ErrorAction SilentlyContinue).Source
    if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }

    $version = (Select-String -Path 'internal\host\version.go' -Pattern 'const Version = "([^"]+)"').Matches[0].Groups[1].Value
    $out = "release\v$version"
    if (Test-Path $out) { Remove-Item -Recurse -Force $out }
    New-Item -ItemType Directory $out | Out-Null

    # These are the programs released, each under its release name.
    # Each updates itself by finding that name in the newest release
    #, so the names must match internal\host\update.go.
    $programs = [ordered]@{
        'dist\windows-amd64\nexus.exe'           = 'nexus_chat-client_Windows.exe'
        'dist\windows-amd64\GuildForge.exe'      = 'GuildForge-Windows_GUI.exe'
        'dist\windows-amd64\guild-forge-cmd.exe' = 'guild-forge_Windows-cmd.exe'
        'dist\linux-amd64\guild-forge'           = 'guild-forge_Debian'
    }
    foreach ($from in $programs.Keys) {
        if (-not (Test-Path $from)) { throw "$from is missing. Run .\build.ps1 first." }
        Copy-Item $from (Join-Path $out $programs[$from])
    }

    # One notices file covers all four. The app and GuildForge's
    # window are built with C and carry the emoji font (the app also the
    # noise model); the command program is pure Go, so everything it holds
    # is already in the others' lists.
    $tags = @()
    if (Test-Path 'internal\assets\files\NotoColorEmoji.ttf') { $tags += 'assetfont' }
    if (Test-Path 'internal\assets\files\rnnoise.bin') { $tags += 'assetmodel' }
    $tags = $tags -join ','
    & $go run ./tools/notices -o "$out\Third-Party_Notices.txt" -goos windows -cgo 1 -tags $tags -title 'Nexus, GuildForge and guild-forge' ./cmd/nexus ./cmd/guild-forge-window ./cmd/guild-forge-cmd
    if ($LASTEXITCODE -ne 0) { throw 'Could not write the notices.' }

    # The user guide goes with each release, for people who prefer one file
    #. Make it first with docs\guide\build-guide.ps1.
    if (-not (Test-Path 'GUIDE.pdf')) { throw 'GUIDE.pdf is missing. Run docs\guide\build-guide.ps1 first.' }
    Copy-Item 'GUIDE.pdf' (Join-Path $out 'Nexus_User-Guide.pdf')

    # The fingerprint list, in the format "sha256sum -c" reads.
    $sums = Get-ChildItem $out -File | Sort-Object Name | ForEach-Object {
        (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower() + '  ' + $_.Name
    }
    [IO.File]::WriteAllText((Join-Path $root "$out\SHA256SUMS.txt"), (($sums -join "`n") + "`n"))

    Write-Host "Made $out with:"
    Get-ChildItem $out -File | ForEach-Object { Write-Host ("  {0,-30} {1,7:N1} MB" -f $_.Name, ($_.Length / 1MB)) }
}
finally {
    Pop-Location
}
