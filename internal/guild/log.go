package guild

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"nexus/internal/guildstore"
)

// OpenLog opens <guild>/logs/guild.log for appending (and also writes to
// the standard error output, which systemd keeps in its journal).
func OpenLog(dataDir string) (*log.Logger, func()) {
	dir := filepath.Join(dataDir, guildstore.LogsDir)
	os.MkdirAll(dir, 0o700)
	f, err := os.OpenFile(filepath.Join(dir, "guild.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	return log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags), func() { f.Close() }
}
