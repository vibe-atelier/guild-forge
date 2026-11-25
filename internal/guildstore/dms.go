package guildstore

import (
	"os"
	"path/filepath"
	"strings"

	"nexus/internal/proto"
)

// Private messages waiting to be collected are kept in
// dms/<recipient>.json. The guild can't read them: they are sealed.

const dmsDirName = "dms"

// LoadDMs reads every waiting queue, by recipient.
func LoadDMs(dataDir string) (map[string][]proto.DMEnvelope, error) {
	out := map[string][]proto.DMEnvelope{}
	entries, err := os.ReadDir(filepath.Join(dataDir, dmsDirName))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		var list []proto.DMEnvelope
		if err := loadJSONFile(filepath.Join(dataDir, dmsDirName), name, &list); err == nil && len(list) > 0 {
			out[strings.TrimSuffix(name, ".json")] = list
		}
	}
	return out, nil
}

// SaveDMs writes one recipient's queue (an empty queue removes the file).
func SaveDMs(dataDir, to string, list []proto.DMEnvelope) error {
	if err := safeID(to); err != nil {
		return err
	}
	dir := filepath.Join(dataDir, dmsDirName)
	if len(list) == 0 {
		err := os.Remove(filepath.Join(dir, to+".json"))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return saveJSONFile(dir, to+".json", list)
}
