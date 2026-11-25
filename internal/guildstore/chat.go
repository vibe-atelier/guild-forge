package guildstore

// This file stores channels, channel keys (sealed boxes) and messages.
//
//	channels.json            the channel list (categories, folders, channels)
//	keys/<channel>.json      the key ledger: current version and sealed boxes
//	messages/<channel>.jsonl messages, one JSON record per line; a later record
//	                         with the same ID replaces an earlier one
//
// Message text and channel keys are only ever stored encrypted.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"nexus/internal/fsutil"
	"nexus/internal/proto"
)

const (
	channelsFileName = "channels.json"
	keysDir          = "keys"
	messagesDir      = "messages"
)

// ChannelKeys is a channel's key ledger.
type ChannelKeys struct {
	Current int            `json:"current"` // It is 0 when there is no key yet.
	Retired bool           `json:"retired"` // It is true when a new key is needed.
	Boxes   []proto.KeyBox `json:"boxes"`
}

// LoadChannels reads channels.json.
func LoadChannels(dataDir string) ([]proto.ChannelNode, error) {
	var n []proto.ChannelNode
	return n, loadJSONFile(dataDir, channelsFileName, &n)
}

// SaveChannels writes channels.json.
func SaveChannels(dataDir string, n []proto.ChannelNode) error {
	return saveJSONFile(dataDir, channelsFileName, n)
}

func safeID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\.:`) {
		return errors.New("bad channel ID")
	}
	return nil
}

// LoadKeys reads a channel's key ledger.
func LoadKeys(dataDir, channel string) (ChannelKeys, error) {
	var k ChannelKeys
	if err := safeID(channel); err != nil {
		return k, err
	}
	return k, loadJSONFile(filepath.Join(dataDir, keysDir), channel+".json", &k)
}

// SaveKeys writes a channel's key ledger.
func SaveKeys(dataDir, channel string, k ChannelKeys) error {
	if err := safeID(channel); err != nil {
		return err
	}
	return saveJSONFile(filepath.Join(dataDir, keysDir), channel+".json", k)
}

func messagesPath(dataDir, channel string) string {
	return filepath.Join(dataDir, messagesDir, channel+".jsonl")
}

// LoadMessages reads a channel's messages, oldest first.
func LoadMessages(dataDir, channel string) ([]proto.ChatMessage, error) {
	if err := safeID(channel); err != nil {
		return nil, err
	}
	f, err := os.Open(messagesPath(dataDir, channel))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var list []proto.ChatMessage
	at := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var m proto.ChatMessage
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == "" {
			continue // A damaged line (for example, one cut off by a crash) is skipped.
		}
		if i, ok := at[m.ID]; ok {
			list[i] = m
		} else {
			at[m.ID] = len(list)
			list = append(list, m)
		}
	}
	return list, sc.Err()
}

// AppendMessage adds (or updates) one message record.
func AppendMessage(dataDir, channel string, m proto.ChatMessage) error {
	if err := safeID(channel); err != nil {
		return err
	}
	path := messagesPath(dataDir, channel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(m)
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// RewriteMessages replaces a channel's file with exactly these messages
// (used to drop old messages and squeeze out replaced records).
func RewriteMessages(dataDir, channel string, list []proto.ChatMessage) error {
	if err := safeID(channel); err != nil {
		return err
	}
	var sb strings.Builder
	for _, m := range list {
		data, _ := json.Marshal(m)
		sb.Write(data)
		sb.WriteByte('\n')
	}
	path := messagesPath(dataDir, channel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, []byte(sb.String()), 0o600)
}

// DeleteChannelData removes a channel's messages and keys.
func DeleteChannelData(dataDir, channel string) {
	if safeID(channel) != nil {
		return
	}
	os.Remove(messagesPath(dataDir, channel))
	os.Remove(filepath.Join(dataDir, keysDir, channel+".json"))
	// The channel's attachments are removed too.
	os.RemoveAll(filepath.Join(dataDir, "attachments", channel))
}
