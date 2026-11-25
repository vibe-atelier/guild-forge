# Takes the pictures used in GUIDE.pdf.
#
# It builds a throwaway demo in C:\NexusGuide (a guild run by GuildForge,
# and two users, Alice and Ben), then starts the Nexus app once per picture:
# the app opens on the right screen, saves a picture of its own window and
# closes. Nothing else on the computer is touched; the demo folder is
# deleted at the end.
#
# Run from the project folder, after .\build.ps1:
#   powershell -ExecutionPolicy Bypass -File docs\guide\screenshots.ps1

$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\..\.."
$img = Join-Path $PSScriptRoot 'img'
New-Item -ItemType Directory -Force $img | Out-Null
$hostExe = Join-Path $root 'dist\windows-amd64\guild-forge-cmd.exe'
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }

$t = 'C:\NexusGuide'
Remove-Item -Recurse -Force $t -ErrorAction SilentlyContinue
New-Item -ItemType Directory "$t\You\Documents" | Out-Null
# The app drawn in memory only (docs\shots), so no window pops up.
$appExe = "$t\shots.exe"
$env:CGO_ENABLED = '1'
& $go build -C $root -o $appExe ./docs/shots
if ($LASTEXITCODE -ne 0) { throw 'Could not build the picture tool (it needs gcc, like the app).' }
Remove-Item Env:CGO_ENABLED
$A = "$t\Alice"; $B = "$t\Ben"
$addr = '127.0.0.1:50000'
$env:GUILD_FORGE_HOME = "$t\host"
$env:GUILD_FORGE_LISTEN = '127.0.0.1'
$env:GUILD_FORGE_ROUTER = 'demo' # a made-up router: pictures never ask the real one or show real addresses

Push-Location $root
# The demo helper is built once, not compiled again for every step.
& $go build -C $root -o "$t\setup.exe" ./docs/guide/setup
if ($LASTEXITCODE -ne 0) { throw 'Could not build the demo helper.' }
function Setup { $out = & "$t\setup.exe" @args; if ($LASTEXITCODE -ne 0) { throw "setup $($args[0]) failed" }; return $out }

# Shot starts the app on one screen and waits for its picture.
function Shot($name, $appHome, $screen = '', $guild = '', $delay = 3000, $password = '') {
    $env:NEXUS_HOME = $appHome
    $env:NEXUS_SCREEN = $screen
    $env:NEXUS_SCREEN_PASSWORD = $password
    $env:NEXUS_SCREEN_GUILD = $guild
    $env:NEXUS_SHOT = Join-Path $img "$name.png"
    $env:NEXUS_SHOT_DELAY = "$delay"
    $env:USERPROFILE = "$t\You" # so paths in the pictures are the demo's own
    $p = Start-Process $appExe -PassThru -NoNewWindow
    if (-not $p.WaitForExit(40000)) { Stop-Process $p -Force; throw "No picture for $name" }
    $env:USERPROFILE = $realProfile
    Write-Host "Saved $name.png"
}
$realProfile = $env:USERPROFILE
function UnlockShot($name, $appHome, $user, $guild = '', $delay = 3500) {
    Shot $name $appHome "unlock:$appHome\users\$user" $guild $delay "$user-pass-2026"
}

try {
    # ---- the guild, broadcasting, without an owner ----
    "guildpass1`nguildpass1`n`n" | & $hostExe create | Out-Null
    "guildpass1`n" | & $hostExe setup=1 port=50000 | Out-Null
    "guildpass1`n" | & $hostExe setup=1 broadcast | Out-Null
    $otp = (& $hostExe OTP | Where-Object { $_ -match '^\s+[A-Z0-9]{3}-' }).Trim()

    # ---- your user ----
    New-Item -ItemType Directory $A, $B | Out-Null
    Shot '01-welcome' $A
    Shot 'app-updates' $A 'updates'
    Shot '02-create-user' $A 'create'
    Setup user $A Alice | Out-Null
    Shot '10-load-user' $A 'load'
    Shot '11-unlock' $A "unlock:$A\users\Alice"
    UnlockShot '03-home' $A Alice 'home'

    # ---- becoming the owner, and the guild's pages ----
    UnlockShot '04-add-guild' $A Alice "home:pair:$addr" 5000
    $gid = Setup pair $A Alice $addr $otp
    UnlockShot '05-server-settings' $A Alice "${gid}:settings"
    UnlockShot '06-about' $A Alice $gid
    UnlockShot '07-guild-menu' $A Alice "${gid}:menu"
    UnlockShot '08-leave-owner' $A Alice "${gid}:leave"
    UnlockShot '12-join-rules' $A Alice "${gid}:joinset" 4500
    UnlockShot '13-captcha' $A Alice "${gid}:captcha" 4500

    # ---- Ben joins ----
    Setup user $B Ben | Out-Null
    UnlockShot '14-join' $B Ben "home:join:$addr" 5500
    UnlockShot '15-quiz' $B Ben "home:quiz:$addr" 5500
    UnlockShot '16-quiz-passed' $B Ben 'home:passed'
    Setup join $B Ben $addr | Out-Null
    UnlockShot '17-waiting' $B Ben $gid 4000
    UnlockShot '18-requests' $A Alice "${gid}:requests" 4500
    Setup approve $A Alice $addr | Out-Null
    UnlockShot '19-members' $A Alice "${gid}:member" 4000

    # ---- channels and chat ----
    Setup chat $A $B $addr | Out-Null
    UnlockShot '20-new-channel' $A Alice "${gid}:newchannel"
    UnlockShot '21-chat' $A Alice "${gid}:chat" 4500

    # ---- roles and permissions ----
    Setup roles $A $B $addr | Out-Null
    UnlockShot '22-roles' $A Alice "${gid}:roles" 4500
    UnlockShot '23-role-edit' $A Alice "${gid}:role" 4500
    UnlockShot '24-channel-rules' $A Alice "${gid}:chanperms" 4500
    UnlockShot '25-member-roles' $A Alice "${gid}:member" 4500

    # ---- contacts and private messages ----
    Setup request $A $B $addr | Out-Null
    UnlockShot '26-request' $B Ben 'home:requests' 4500
    Setup contacts $A $B $addr | Out-Null
    UnlockShot '27-dm-chat' $A Alice 'home:dm' 5000
    UnlockShot '28-verify' $B Ben 'home:verify' 5000
    UnlockShot '29-member-contact' $A Alice "${gid}:member" 4500

    # ---- Calendar, Threads, Wiki ----
    Setup pages $A $B $addr | Out-Null
    UnlockShot '31-calendar' $A Alice "${gid}:calendar" 4500
    UnlockShot '32-event' $A Alice "${gid}:event" 4500
    UnlockShot '33-thread' $A Alice "${gid}:post" 4500
    UnlockShot '34-wiki' $A Alice "${gid}:wikipage" 5500
    UnlockShot '35-rules-notice' $B Ben $gid 4500
    Setup media $A $B $addr | Out-Null
    UnlockShot '36-chat-media' $A Alice "${gid}:chat" 6000
    UnlockShot '37-emoji' $A Alice "${gid}:emoji" 5000
    Setup profiles $A $B $addr | Out-Null
    UnlockShot '39-edit-profile' $B Ben 'home:editprofile' 5000
    UnlockShot '41-notify' $A Alice "${gid}:chnotify" 6000
    UnlockShot '42-recovery' $A Alice 'home:recovery' 4000
    Setup currency $A $B $addr | Out-Null
    UnlockShot '43-wallet' $B Ben "${gid}:wallet" 5000
    UnlockShot '44-bank' $A Alice "${gid}:coinbank" 5500
    UnlockShot '45-shop' $B Ben "${gid}:shop" 5000

    # ---- a guild that can't be reached ----
    & $hostExe stop=1 | Out-Null
    UnlockShot '09-offline' $A Alice $gid 4000
}
finally {
    $env:USERPROFILE = $realProfile
    Pop-Location
    & $hostExe stop=1 2>$null | Out-Null
    Get-CimInstance Win32_Process -Filter "Name='guild-forge-cmd.exe'" | Where-Object { $_.CommandLine -like '*NexusGuide*' -or $_.ExecutablePath -eq $hostExe } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Start-Sleep 1
    Remove-Item -Recurse -Force $t -ErrorAction SilentlyContinue
    foreach ($v in 'NEXUS_HOME', 'NEXUS_SCREEN', 'NEXUS_SCREEN_PASSWORD', 'NEXUS_SCREEN_GUILD', 'NEXUS_SHOT', 'NEXUS_SHOT_DELAY', 'GUILD_FORGE_HOME', 'GUILD_FORGE_LISTEN') { Remove-Item "Env:$v" -ErrorAction SilentlyContinue }
}
Write-Host "Done. Pictures are in $img"
