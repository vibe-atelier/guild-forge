package guildstore

// This file keeps the guild's public profile (About page) and member list, stored in the
// guild folder by the running guild.

import (
	"nexus/internal/fsutil"

	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // This lets pictures in JPG be checked.
	_ "image/png"  // This lets pictures in PNG be checked.
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	profileFileName = "guild.json"
	membersFileName = "members.json"
	pictureFileName = "guild-picture" // The file is inside media/.

	DefaultGuildName = "New Guild" // This is the name of a new guild.
	MaxGuildName     = 64
	MaxAbout         = 2000
	MaxPictureBytes  = 2 * 1024 * 1024
	MaxPictureSide   = 512 // A guild picture is at most 512 pixels a side.
)

// GuildProfile is guild.json: what the About page shows.
type GuildProfile struct {
	Name            string `json:"name"`
	About           string `json:"about"`
	PictureHash     string `json:"picture_hash,omitempty"`
	ShowMemberCount bool   `json:"show_member_count"`
	DefaultNotify   string `json:"default_notify,omitempty"` // It is "all" or "mentions".
}

// Member is one entry of members.json.
type Member struct {
	UserKey string    `json:"user_key"`
	Name    string    `json:"name,omitempty"` // It is the name as sent by their app.
	Joined  time.Time `json:"joined"`
	LastIP  string    `json:"last_ip,omitempty"` // It is kept for "ban the address too".
	// It is the X25519 public key for sealed channel keys.
	EncKey []byte   `json:"enc_key,omitempty"`
	Typing string   `json:"typing,omitempty"` // It is the custom "is typing" text.
	Roles  []string `json:"roles,omitempty"`  // These are role IDs; Everyone is implied.
	// It is true when they joined by ticking "I agree".
	AgreedRules   bool      `json:"agreed_rules,omitempty"`
	Nick          string    `json:"nick,omitempty"`         // It is their name in this guild.
	Bio           string    `json:"bio,omitempty"`          // It is their bio.
	PictureHash   string    `json:"picture_hash,omitempty"` // The picture is kept in profiles/.
	BannerHash    string    `json:"banner_hash,omitempty"`
	Timeout       time.Time `json:"timeout_until,omitzero"`   // They can't post until then.
	VoiceMuted    bool      `json:"voice_muted,omitempty"`    // It is set by a moderator.
	VoiceDeafened bool      `json:"voice_deafened,omitempty"` // It is set by a moderator.
	// It is when the account was created, as its app said.
	Created time.Time `json:"created,omitzero"`
	// It is how many guilds the account is in, as its app last said.
	Guilds int `json:"guilds,omitempty"`
}

// LoadProfile reads the guild's About information and its picture (nil when
// it has none). A guild with no profile yet gets the defaults.
func LoadProfile(dataDir string) (GuildProfile, []byte, error) {
	p := GuildProfile{Name: DefaultGuildName, ShowMemberCount: true}
	data, err := os.ReadFile(filepath.Join(dataDir, profileFileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &p); err != nil {
			return p, nil, fmt.Errorf("%s is damaged", profileFileName)
		}
	}
	var pic []byte
	if p.PictureHash != "" {
		pic, _ = os.ReadFile(filepath.Join(dataDir, "media", pictureFileName))
		if PictureHash(pic) != p.PictureHash {
			pic, p.PictureHash = nil, ""
		}
	}
	return p, pic, nil
}

// SaveProfile writes the guild's About information.
func SaveProfile(dataDir string, p GuildProfile) error {
	data, _ := json.MarshalIndent(p, "", "  ")
	return fsutil.WriteFileAtomic(filepath.Join(dataDir, profileFileName), append(data, '\n'), 0o600)
}

// SavePicture writes the guild's picture (nil removes it).
func SavePicture(dataDir string, pic []byte) error {
	dir := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if pic == nil {
		err := os.Remove(filepath.Join(dir, pictureFileName))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, pictureFileName), pic, 0o600)
}

// PictureHash is a short fingerprint of a picture, so apps can tell whether
// theirs is current ("" for no picture).
func PictureHash(pic []byte) string {
	if len(pic) == 0 {
		return ""
	}
	sum := sha256.Sum256(pic)
	return hex.EncodeToString(sum[:16])
}

// LoadMembers reads the member list.
func LoadMembers(dataDir string) ([]Member, error) {
	var list []Member
	data, err := os.ReadFile(filepath.Join(dataDir, membersFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%s is damaged", membersFileName)
	}
	return list, nil
}

// SaveMembers writes the member list.
func SaveMembers(dataDir string, list []Member) error {
	data, _ := json.MarshalIndent(list, "", "  ")
	return fsutil.WriteFileAtomic(filepath.Join(dataDir, membersFileName), append(data, '\n'), 0o600)
}

// RemoveOwner deletes owner.json (the guild has no owner afterwards).
func RemoveOwner(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, ownerFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// CheckGuildName validates a new guild name.
func CheckGuildName(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return "", errors.New("the guild needs a name")
	case n > MaxGuildName:
		return "", fmt.Errorf("guild names can be at most %d characters", MaxGuildName)
	}
	if strings.ContainsAny(s, "\r\n\t\x00") {
		return "", errors.New("guild names cannot contain line breaks")
	}
	return s, nil
}

// CheckPicture makes sure pic is a PNG or JPG of at most 512×512.
// The app shrinks pictures before sending; the guild only checks.
func CheckPicture(pic []byte) error {
	if len(pic) > MaxPictureBytes {
		return errors.New("the picture is too large")
	}
	return checkImage(pic, MaxPictureSide)
}

// checkImage makes sure img is a PNG or JPG of at most side×side pixels.
func checkImage(img []byte, side int) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || (format != "png" && format != "jpeg") {
		return errors.New("the picture must be a PNG or JPG image")
	}
	if cfg.Width > side || cfg.Height > side || cfg.Width < 1 || cfg.Height < 1 {
		return fmt.Errorf("the picture must be at most %d×%d", side, side)
	}
	return nil
}
