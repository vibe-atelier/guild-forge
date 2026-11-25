//go:build assetmodel

package assets

import (
	_ "embed" // This import is needed for go:embed.

	"nexus/internal/nexusdata"
)

// These are the noise model and RNNoise's licence (BSD).
var (
	//go:embed files/rnnoise.bin
	noiseModel []byte
	//go:embed files/COPYING-RNNoise.txt
	modelLicence []byte
)

func init() {
	files = append(files,
		file{nexusdata.ModelsPart, "rnnoise.bin", noiseModel},
		file{nexusdata.ModelsPart, "COPYING-RNNoise.txt", modelLicence})
}
