# Remakes vpxasm_windows_amd64.syso: libvpx's fast routines written in
# assembly, put together by NASM (nasm.us) for 64-bit Windows.
# The file is kept in the project, so building Nexus doesn't need NASM; run
# this only after changing libvpx. Needs nasm.exe (in the nexus folder, in
# nasm-3.02-win64\nasm-3.02, or on PATH) and MSYS2's ld (C:\msys64\mingw64\bin).
#   .\internal\vp8\asm.ps1
$ErrorActionPreference = 'Stop'

$nasm = (Get-Command nasm -ErrorAction SilentlyContinue).Source
if (-not $nasm) { $nasm = Join-Path $PSScriptRoot '..\..\nasm-3.02-win64\nasm-3.02\nasm.exe' }
if (-not (Test-Path $nasm)) { throw 'NASM was not found. Get it from https://www.nasm.us' }
$ld = (Get-Command ld -ErrorAction SilentlyContinue).Source
if (-not $ld) { $ld = 'C:\msys64\mingw64\bin\ld.exe' }
if (-not (Test-Path $ld)) { throw 'ld was not found. Install MSYS2 (see build.ps1).' }

# The assembly files libvpx's own build uses for VP8 on x86-64.
$files = @(
    "vp8/common/x86/dequantize_mmx.asm",
    "vp8/common/x86/idctllm_mmx.asm",
    "vp8/common/x86/idctllm_sse2.asm",
    "vp8/common/x86/iwalsh_sse2.asm",
    "vp8/common/x86/loopfilter_block_sse2_x86_64.asm",
    "vp8/common/x86/loopfilter_sse2.asm",
    "vp8/common/x86/recon_mmx.asm",
    "vp8/common/x86/recon_sse2.asm",
    "vp8/common/x86/subpixel_mmx.asm",
    "vp8/common/x86/subpixel_sse2.asm",
    "vp8/common/x86/subpixel_ssse3.asm",
    "vp8/encoder/x86/block_error_sse2.asm",
    "vp8/encoder/x86/copy_sse2.asm",
    "vp8/encoder/x86/copy_sse3.asm",
    "vp8/encoder/x86/dct_sse2.asm",
    "vp8/encoder/x86/fwalsh_sse2.asm",
    "vpx_dsp/x86/intrapred_sse2.asm",
    "vpx_dsp/x86/intrapred_ssse3.asm",
    "vpx_dsp/x86/sad4d_sse2.asm",
    "vpx_dsp/x86/sad_sse2.asm",
    "vpx_dsp/x86/ssim_opt_x86_64.asm",
    "vpx_dsp/x86/subpel_variance_sse2.asm",
    "vpx_dsp/x86/subtract_sse2.asm",
    "vpx_ports/emms_mmx.asm"
)

$work = Join-Path ([IO.Path]::GetTempPath()) "nexus-vpxasm"
Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $work | Out-Null
Push-Location (Join-Path $PSScriptRoot 'libvpx')
try {
    $objects = @()
    foreach ($f in $files) {
        $o = Join-Path $work (($f -replace '/', '_') + '.o')
        & $nasm -f win64 -I./ -Iconfig_x86_64/ -o $o $f
        if ($LASTEXITCODE -ne 0) { throw "NASM failed on $f" }
        $objects += $o
    }
    & $ld -r -o (Join-Path $PSScriptRoot 'vpxasm_windows_amd64.syso') @objects
    if ($LASTEXITCODE -ne 0) { throw 'ld failed' }
    Write-Host "Made internal\vp8\vpxasm_windows_amd64.syso"
}
finally {
    Pop-Location
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
