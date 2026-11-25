# Turns docs\guide\guide.html into GUIDE.pdf (in the project folder) using
# Microsoft Edge's built-in "print to PDF", and into the GitHub README with
# its pictures in release\github. Run from the project folder:
#   powershell -ExecutionPolicy Bypass -File docs\guide\build-guide.ps1
# To refresh the screenshots first, run docs\guide\screenshots.ps1.

$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\..\.."
$html = Join-Path $PSScriptRoot 'guide.html'
$pdf = Join-Path $root 'GUIDE.pdf'
$edge = "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe"
if (-not (Test-Path $edge)) { $edge = "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe" }

$profileDir = Join-Path $env:TEMP 'nexus-guide-pdf'
$url = 'file:///' + ($html -replace '\\', '/')
$p = Start-Process $edge -PassThru -Wait -WindowStyle Hidden -ArgumentList @(
    '--headless=new', '--disable-gpu', '--no-pdf-header-footer', '--no-first-run',
    "--user-data-dir=$profileDir", "--print-to-pdf=$pdf", $url)
Remove-Item -Recurse -Force $profileDir -ErrorAction SilentlyContinue
if (-not (Test-Path $pdf)) { throw 'Edge did not make the PDF' }
Write-Host ("Made {0} ({1:N0} KB)" -f $pdf, ((Get-Item $pdf).Length / 1KB))

# The same guide as the GitHub repository's front page: README.md and
# its pictures in release\github, ready to upload.
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = 'C:\Program Files\Go\bin\go.exe' }
Push-Location $root
try {
    & $go run ./tools/guide-md -o 'release\github'
    if ($LASTEXITCODE -ne 0) { throw 'Could not write the GitHub README.' }
}
finally {
    Pop-Location
}
