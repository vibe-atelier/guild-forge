#!/bin/sh
# Builds Nexus on Debian into dist/linux-amd64/:
#   nexus           the desktop app
#   guild-forge     the guild host (GuildForge), commands only
#   nexus-devclient the small test client
#
# Needs Go (go.dev/dl). The app (its window, voice and camera) also needs a C compiler and
# the graphics libraries:
#   sudo apt install gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev libasound2-dev
# Then, in this folder:   sh build.sh
#
# "sh build.sh install" also puts the app in the desktop's menu, for you
# only: the program in ~/.local/bin, its logo in
# ~/.local/share/icons and its menu entry in ~/.local/share/applications.
# Nothing outside your home folder is changed, and no sudo is needed.
set -e
cd "$(dirname "$0")"
out=dist/linux-amd64
mkdir -p "$out"

# Strong noise suppression: RNNoise's model as a weights file, made
# from Xiph's model when it isn't made yet.
model=rnnoise-v0.2/rnnoise-v0.2/src/rnnoise_data.c
if [ ! -f models/rnnoise.bin ] && [ -f "$model" ]; then
	mkdir -p models
	go run ./tools/rnnoise-weights "$model" models/rnnoise.bin
fi

# The emoji font and the noise model are built into the programs,
# which write them into the PC's nexus-data when they start. The
# files are put where the assets package builds them in; whatever is
# missing is simply left out.
assets=internal/assets/files
mkdir -p "$assets"
apptags=""
for font in fonts/NotoColorEmoji.ttf NotoColorEmoji.ttf; do
	if [ -f "$font" ]; then
		cp "$font" "$assets/NotoColorEmoji.ttf"
		cp fonts/OFL.txt "$assets/OFL.txt"
		apptags="assetfont"
		break
	fi
done
if [ -f models/rnnoise.bin ]; then
	cp models/rnnoise.bin "$assets/rnnoise.bin"
	cp internal/rnnoise/csrc/COPYING "$assets/COPYING-RNNoise.txt"
	apptags="${apptags:+$apptags,}assetmodel"
fi

CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$out/nexus-devclient" ./cmd/nexus-devclient
echo "Built $out/nexus-devclient"
# GuildForge on Linux is commands only: pure Go, with no C compiler needed.
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$out/guild-forge" ./cmd/guild-forge-cmd
echo "Built $out/guild-forge"
if command -v gcc >/dev/null 2>&1; then
	# The app carries the emoji font, the noise model and its notices for its
	# Licenses pages.
	CGO_ENABLED=1 go run ./tools/notices -o cmd/nexus/NOTICES.txt -goos linux -cgo 1 -tags "$apptags" -title Nexus ./cmd/nexus
	CGO_ENABLED=1 go build -trimpath -tags "${apptags:+$apptags,}notices" -ldflags '-s -w' -o "$out/nexus" ./cmd/nexus
	echo "Built $out/nexus"
else
	echo "Skipped the app (nexus): no C compiler. Run: sudo apt install gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev libasound2-dev"
fi
if [ -n "$apptags" ]; then
	echo "Built in: $apptags"
fi
# The programs no longer need fonts and models folders beside them.
rm -rf "$out/fonts" "$out/models"

# Putting the app in the menu, only when asked with "install".
if [ "$1" = "install" ]; then
	if [ ! -f "$out/nexus" ]; then
		echo "The app was not built, so nothing was installed."
		exit 1
	fi
	bin="$HOME/.local/bin"
	icons="$HOME/.local/share/icons/hicolor/scalable/apps"
	apps="$HOME/.local/share/applications"
	mkdir -p "$bin" "$icons" "$apps"
	install -m 755 "$out/nexus" "$bin/nexus"
	install -m 644 internal/ui/logo.svg "$icons/nexus.svg"
	# The menu entry names the program by its full path, so it works even
	# when ~/.local/bin is not on the list of places commands are found.
	cat > "$apps/nexus.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Nexus
Comment=Decentralised chat
Exec=$bin/nexus
Icon=nexus
Terminal=false
Categories=Network;Chat;InstantMessaging;
EOF
	chmod 644 "$apps/nexus.desktop"
	echo "Installed Nexus: find it in your desktop's app menu or app list."
	echo "To remove it: rm $bin/nexus $icons/nexus.svg $apps/nexus.desktop"
fi
