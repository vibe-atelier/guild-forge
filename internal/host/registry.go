// Package host is the guild-forge command: the list of guilds on this
// computer (the registry) and every command that manages them. The running
// guild itself is in package guild; its files are in package guildstore.
package host

import (
	"nexus/internal/fsutil"
	"nexus/internal/guildstore"
	"nexus/internal/platform"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// MaxGuilds is how many guilds one computer can run.
const MaxGuilds = 8

const registryFileName = "registry.json"

// Entry is one guild slot in the registry. The guild's own data
// (password, keys, owner, chats and media) lives in its folder.
type Entry struct {
	Index int `json:"index"`
	// GuildID is the guild's public key. Unlike Index it never
	// changes, so services, boot entries and backups are named after it.
	GuildID     string `json:"guild_id"`
	DataDir     string `json:"data_dir"`
	Port        int    `json:"port,omitempty"` // It is 0 when no port is assigned yet.
	StartOnBoot bool   `json:"start_on_boot"`
	NoAutoPort  bool   `json:"no_auto_port,omitempty"` // It is true when Port Forwarding is off.
	AutoBackups string `json:"auto_backups,omitempty"` // It is "", daily, weekly or monthly.
}

// Name is how the guild is shown, e.g. GUILD#1@50000.
func (e *Entry) Name(ownerKey string) string { return DisplayName(e.Index, e.Port, ownerKey) }

// Registry is the record of all guilds on this computer.
type Registry struct {
	Version int     `json:"version"`
	Guilds  []Entry `json:"guilds"`

	dir string // It is guild-forge's own folder, where the registry is saved.
}

// ConfigDir is guild-forge's own folder. The GUILD_FORGE_HOME
// environment variable overrides it (used for testing).
func ConfigDir() (string, error) {
	if d := os.Getenv("GUILD_FORGE_HOME"); d != "" {
		return d, nil
	}
	return platform.DefaultConfigDir()
}

// LoadRegistry reads the registry from dir. A missing file means no guilds yet.
func LoadRegistry(dir string) (*Registry, error) {
	r := &Registry{Version: 1, dir: dir}
	path := filepath.Join(dir, registryFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s is damaged (%v)", path, err)
	}
	seen := map[int]bool{}
	for _, e := range r.Guilds {
		if e.Index < 1 || e.Index > MaxGuilds || seen[e.Index] {
			return nil, fmt.Errorf("%s is damaged (bad guild index %d)", path, e.Index)
		}
		seen[e.Index] = true
	}
	r.sort()
	return r, nil
}

// OpenRegistry loads the registry and removes guilds whose folder has
// disappeared, renumbering the rest. It reports what it did
// to out. All management commands start with this.
func OpenRegistry(dir string, out io.Writer) (*Registry, error) {
	r, err := LoadRegistry(dir)
	if err != nil {
		return nil, err
	}
	changed := false
	kept := r.Guilds[:0]
	for _, e := range r.Guilds {
		if guildstore.HasGuildData(e.DataDir) {
			if e.GuildID == "" { // Registries from before GuildID existed lack it.
				if gd, err := guildstore.LoadGuildData(e.DataDir); err == nil {
					e.GuildID = gd.PublicKey
					changed = true
				}
			}
			kept = append(kept, e)
			continue
		}
		fmt.Fprintf(out, "GUILD#%d's folder is gone (%s), so it has been removed from the list.\n", e.Index, e.DataDir)
		if e.GuildID != "" {
			platform.ForgetService(e.GuildID)
		}
		changed = true
	}
	r.Guilds = kept
	if r.compact(out) {
		changed = true
	}
	if changed {
		if err := r.Save(); err != nil {
			return nil, fmt.Errorf("cannot save the guild list: %w", err)
		}
	}
	return r, nil
}

// compact renumbers the guilds 1, 2, 3, ... keeping their order.
func (r *Registry) compact(out io.Writer) bool {
	r.sort()
	changed := false
	for i := range r.Guilds {
		want := i + 1
		if r.Guilds[i].Index != want {
			if out != nil {
				fmt.Fprintf(out, "GUILD#%d is now GUILD#%d.\n", r.Guilds[i].Index, want)
			}
			r.Guilds[i].Index = want
			changed = true
		}
	}
	return changed
}

// Save writes the registry to disk safely.
func (r *Registry) Save() error {
	r.sort()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir, registryFileName)
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), 0o640); err != nil {
		return err
	}
	if err := platform.ShareWithGuildUser(path); err != nil {
		return err
	}
	// Start on Boot is kept here; the system's own sign-in task follows it.
	return platform.SyncBoot(r.anyStartsOnBoot(), true)
}

// anyStartsOnBoot reports whether any guild is set to start on boot.
func (r *Registry) anyStartsOnBoot() bool {
	for _, e := range r.Guilds {
		if e.StartOnBoot {
			return true
		}
	}
	return false
}

// Get returns the entry with the given index, or nil.
func (r *Registry) Get(index int) *Entry {
	for i := range r.Guilds {
		if r.Guilds[i].Index == index {
			return &r.Guilds[i]
		}
	}
	return nil
}

// GetByID returns the entry with the given guild ID, or nil.
func (r *Registry) GetByID(id string) *Entry {
	for i := range r.Guilds {
		if r.Guilds[i].GuildID == id {
			return &r.Guilds[i]
		}
	}
	return nil
}

// NextIndex is the index a new guild gets: the one after the last.
// ok is false when all slots are used.
func (r *Registry) NextIndex() (index int, ok bool) {
	if len(r.Guilds) >= MaxGuilds {
		return 0, false
	}
	return len(r.Guilds) + 1, true
}

// UsesDataDir reports which guild, if any, already uses dir.
func (r *Registry) UsesDataDir(dir string) (index int, used bool) {
	for _, e := range r.Guilds {
		if fsutil.SamePath(e.DataDir, dir) {
			return e.Index, true
		}
	}
	return 0, false
}

// PortUsedBy reports which guild, if any, has port assigned.
func (r *Registry) PortUsedBy(port int) (index int, used bool) {
	for _, e := range r.Guilds {
		if e.Port == port {
			return e.Index, true
		}
	}
	return 0, false
}

// Add puts a new guild at the end of the list.
func (r *Registry) Add(e Entry) (*Entry, error) {
	index, ok := r.NextIndex()
	if !ok {
		return nil, fmt.Errorf("all %d guild slots are in use; delete one first with guild-forge delete=N", MaxGuilds)
	}
	if e.GuildID != "" {
		if other := r.GetByID(e.GuildID); other != nil {
			return nil, fmt.Errorf("this guild is already on this computer as GUILD#%d", other.Index)
		}
	}
	e.Index = index
	r.Guilds = append(r.Guilds, e)
	return &r.Guilds[len(r.Guilds)-1], nil
}

// Remove takes guild index off the list and renumbers the rest.
func (r *Registry) Remove(index int, out io.Writer) {
	kept := r.Guilds[:0]
	for _, e := range r.Guilds {
		if e.Index != index {
			kept = append(kept, e)
		}
	}
	r.Guilds = kept
	r.compact(out)
}

func (r *Registry) sort() {
	sort.Slice(r.Guilds, func(i, j int) bool { return r.Guilds[i].Index < r.Guilds[j].Index })
}
