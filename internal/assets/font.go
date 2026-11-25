//go:build assetfont

package assets

import (
	_ "embed" // This import is needed for go:embed.

	"nexus/internal/nexusdata"
)

// These are the emoji font and its licence (OFL 1.1).
var (
	//go:embed files/NotoColorEmoji.ttf
	emojiFont []byte
	//go:embed files/OFL.txt
	fontLicence []byte
)

func init() {
	files = append(files,
		file{nexusdata.FontsPart, "NotoColorEmoji.ttf", emojiFont},
		file{nexusdata.FontsPart, "OFL.txt", fontLicence})
}
