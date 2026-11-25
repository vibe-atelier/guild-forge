# Building Nexus from the source code

The programs are built with **Go** (go.dev/dl). The app and GuildForge's
window also need a **C compiler**, because their window, voice and video
parts are partly written in C. Everything else is downloaded by Go itself
the first time, and checked against the fingerprints in `go.sum`.

## On Windows

1. Install Go from https://go.dev/dl.
2. Install a C compiler: install MSYS2 from https://www.msys2.org, open
   **MSYS2 MinGW x64**, run `pacman -S mingw-w64-x86_64-toolchain`, and add
   `C:\msys64\mingw64\bin` to your PATH.
3. In PowerShell, in this folder, run:

   ```powershell
   .\build.ps1
   ```

The programs appear in `dist\windows-amd64\` (the app `nexus.exe`,
`GuildForge.exe` and `guild-forge-cmd.exe`) and `dist\linux-amd64\`
(`guild-forge` for Linux, which needs no C compiler).

## On Debian-based Linux

The guide's chapter **Building the app on Linux** (in `README.md`) walks
through it step by step, with every command explained. In short:

```bash
sudo apt install gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev libasound2-dev
sh build.sh install
```

`sh build.sh` builds into `dist/linux-amd64/`; with `install`, it also puts
the app in your desktop's app menu, for you only.

## Optional extras

Two files are left out of the source code because they are large and
belong to others. Without them, everything still builds:

- **Noto Color Emoji**, for newer emoji: see `fonts/README.md`.
- **The RNNoise model**, for Strong noise suppression: see
  `models/README.md`.

## Licences

Nexus's own code is under the MIT licence, in `LICENSE`. The third-party
code inside it (in `internal/opus/libopus`, `internal/vp8/libvpx`,
`internal/speexdsp/libspeexdsp` and `internal/rnnoise/csrc`) keeps its own
licence, in the files beside it, and the Go libraries it uses keep theirs.
`Third-Party_Notices.txt` in each release lists them all.

## Checking the code

```bash
go vet ./...
go test ./...
```

## Other scripts

| Script | What it does |
|---|---|
| `tools/release.ps1` | Collects a release's files and their notices in `release\`. |
| `tools/make-icon` | Remakes the Windows programs' icon and details. |
| `tools/notices` | Writes the third-party notices of a program. |
| `tools/rnnoise-weights` | Turns the RNNoise model into the file the app reads. |
| `tools/guide-md` | Turns the guide into this repository's `README.md`. |
| `docs/guide/build-guide.ps1` | Makes the guide as a PDF and as `README.md`. |
| `docs/guide/screenshots.ps1` | Takes the guide's pictures, from a made-up demo. |
| `internal/vp8/asm.ps1` | Rebuilds the video code's assembly parts (needs NASM). |
