package guild

import (
	"testing"

	"nexus/internal/guildstore"
)

// newGuildDir makes a guild folder with fresh keys, as "guild-forge create"
// would, and returns it.
func newGuildDir(t *testing.T, password string) string {
	t.Helper()
	dir := t.TempDir()
	gd, err := guildstore.NewGuildData(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := gd.Save(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}
