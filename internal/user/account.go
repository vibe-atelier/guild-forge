package user

// This file has changing the password and name, the recovery code, and the
// profile others see.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const MaxBio = 190 // It is in characters.

// ChangePassword seals the keys again with a new password. The user
// ID and everything else stay the same.
func (u *User) ChangePassword(old, new string) error {
	salt, err := saltBytes(&u.Profile)
	if err != nil {
		return err
	}
	_, hash, err := deriveKeys(old, salt)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(u.Profile.PasswordHash)) != 1 {
		return errors.New("the current password is wrong")
	}
	return u.setPassword(new)
}

func (u *User) setPassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	}
	// A new password always gets a new random salt.
	salt := newSalt()
	saltRaw, _ := base64.RawStdEncoding.DecodeString(salt)
	encKey, hash, err := deriveKeys(password, saltRaw)
	if err != nil {
		return err
	}
	keep, keepKey := u.Profile, u.keyEncKey
	u.keyEncKey, u.Profile.PasswordHash = encKey, hash
	u.Profile.Salt, u.Profile.FormatVersion = salt, saltFormat
	if err := u.sealKeys(); err != nil {
		u.Profile, u.keyEncKey = keep, keepKey
		return err
	}
	if err := u.Save(); err != nil {
		u.Profile, u.keyEncKey = keep, keepKey
		return err
	}
	return nil
}

// SetName changes the default name.
func (u *User) SetName(name string) error {
	name, err := CheckName(name)
	if err != nil {
		return err
	}
	u.Profile.DefaultName = name
	return u.Save()
}

// ---- recovery code ---------------------------------------------------------

// recoveryKey turns a recovery code into the key that seals the second copy
// of the private keys.
func recoveryKey(userID, code string) ([]byte, error) {
	code = NormalizeRecoveryCode(code)
	if len(code) != 32 {
		return nil, errors.New("a recovery code has 32 letters and digits")
	}
	return pbkdf2.Key(sha256.New, code, []byte("nexus-recovery-v1|"+userID), kdfRounds, 32)
}

// NormalizeRecoveryCode removes spaces and dashes and makes it upper case.
func NormalizeRecoveryCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' {
			return -1
		}
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, code)
}

// MakeRecoveryCode makes a new recovery code, shown once, and keeps a copy
// of the keys sealed with it. A new code replaces the old one.
func (u *User) MakeRecoveryCode() (string, error) {
	raw := make([]byte, 20) // This is 160 bits, which make 32 base32 characters.
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := keyText.EncodeToString(raw)
	key, err := recoveryKey(u.Profile.UserID, code)
	if err != nil {
		return "", err
	}
	plain, _ := json.Marshal(privateKeys{SigningSeed: u.SigningKey.Seed(), DecryptionKey: u.DecryptionKey.Bytes()})
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	u.Profile.RecoveryKeys = &sealedBox{Nonce: nonce, Data: aead.Seal(nil, nonce, plain, []byte(u.Profile.UserID+"|recovery"))}
	if err := u.Save(); err != nil {
		return "", err
	}
	var groups []string
	for i := 0; i < len(code); i += 4 {
		groups = append(groups, code[i:i+4])
	}
	return strings.Join(groups, "-"), nil
}

// HasRecoveryCode reports whether a recovery code was made.
func (u *User) HasRecoveryCode() bool { return u.Profile.RecoveryKeys != nil }

// Recover opens the user in dir with its recovery code and sets a new
// password (Forgot Password).
func Recover(dir, code, newPassword string) (*User, error) {
	p, err := ReadProfile(dir)
	if err != nil {
		return nil, err
	}
	if p.RecoveryKeys == nil {
		return nil, errors.New("this user has no recovery code")
	}
	if utf8.RuneCountInString(newPassword) < MinPasswordLength {
		return nil, fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	}
	key, err := recoveryKey(p.UserID, code)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, p.RecoveryKeys.Nonce, p.RecoveryKeys.Data, []byte(p.UserID+"|recovery"))
	if err != nil {
		return nil, errors.New("that recovery code is wrong")
	}
	u, err := fromKeys(dir, p, plain)
	if err != nil {
		return nil, err
	}
	return u, u.setPassword(newPassword)
}

// ---- the profile others see -------------------------------------------------

const (
	pictureFile = "picture" // It is in media/profile.
	bannerFile  = "banner"
)

// SetLook saves the bio, and the picture and banner when given (nil keeps
// them; clear removes them).
func (u *User) SetLook(bio string, picture, banner []byte, clearPicture, clearBanner bool) error {
	bio = strings.TrimSpace(bio)
	if utf8.RuneCountInString(bio) > MaxBio {
		return fmt.Errorf("the bio can be at most %d characters", MaxBio)
	}
	dir := filepath.Join(u.Dir, "media", "profile")
	os.MkdirAll(dir, 0o700)
	put := func(name string, data []byte, clear bool, field *string) error {
		switch {
		case len(data) > 0:
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				return err
			}
			*field = name
		case clear:
			os.Remove(filepath.Join(dir, name))
			*field = ""
		}
		return nil
	}
	if err := put(pictureFile, picture, clearPicture, &u.Profile.ProfilePicture); err != nil {
		return err
	}
	if err := put(bannerFile, banner, clearBanner, &u.Profile.Banner); err != nil {
		return err
	}
	u.Profile.Bio = bio
	return u.Save()
}

// Picture returns your profile picture, or nil.
func (u *User) Picture() []byte { return u.lookFile(u.Profile.ProfilePicture) }

// BannerPicture returns your banner, or nil.
func (u *User) BannerPicture() []byte { return u.lookFile(u.Profile.Banner) }

func (u *User) lookFile(name string) []byte {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return nil
	}
	b, _ := os.ReadFile(filepath.Join(u.Dir, "media", "profile", name))
	return b
}

// SetMuteSounds turns notification sounds off or on.
func (u *User) SetMuteSounds(mute bool) error {
	u.Profile.MuteSounds = mute
	return u.Save()
}

// Voice is how voice works on this computer.
type Voice struct {
	Input       string  `json:"input,omitempty"`       // It is the microphone ID ("" means the default).
	Output      string  `json:"output,omitempty"`      // It is the speaker ID ("" means the default).
	Sensitivity float64 `json:"sensitivity,omitempty"` // It is the voice activity level in dB (0 means -45).
	PushToTalk  bool    `json:"push_to_talk,omitempty"`
	PTTKey      string  `json:"ptt_key,omitempty"` // It is a key name ("" means the ` key).
	// It is Push-To-Blur: while it is held, a shared screen is blurred ("" means none).
	BlurKey string `json:"blur_key,omitempty"`
	// It is Push-To-Mute: while it is held, a shared screen's sound is silent.
	MuteKey string `json:"mute_key,omitempty"`
	// These are the voice clean-up settings, all on unless turned off.
	NoEchoCancel bool `json:"no_echo_cancel,omitempty"`
	NoDenoise    bool `json:"no_denoise,omitempty"` // It is the older setting for Off.
	// Noise Suppression: "off", "standard" (SpeexDSP) or "strong"
	// (RNNoise); "" is Strong when the model is there, else Standard.
	Denoise    string `json:"denoise,omitempty"`
	NoAutoGain bool   `json:"no_auto_gain,omitempty"`
	// These are the voice extras.
	// It is the Priority Speaker key ("" means none).
	PriorityKey string `json:"priority_key,omitempty"`
	// It holds volumes per person (by user ID), in % (100 when missing).
	Volumes map[string]int `json:"volumes,omitempty"`
	// It is how far the soundboard is turned down, in % (100 means off).
	SoundboardCut int `json:"soundboard_cut,omitempty"`
	// These are the camera settings.
	Camera string `json:"camera,omitempty"` // It is the camera ID ("" means the first one).
	// It is where your camera goes while sharing: top-left, top-right, bottom-left
	// or bottom-right ("" means bottom-right).
	CameraCorner string `json:"camera_corner,omitempty"`
}

// VolumeOf is how loud someone is played, in % (0 to 200).
func (v Voice) VolumeOf(userID string) int {
	if n, ok := v.Volumes[userID]; ok {
		return max(0, min(200, n))
	}
	return 100
}

// WithVolume returns the settings with someone's volume changed (the map is
// copied, so settings in use elsewhere don't change under them).
func (v Voice) WithVolume(userID string, percent int) Voice {
	m := map[string]int{}
	for k, n := range v.Volumes {
		m[k] = n
	}
	if percent == 100 {
		delete(m, userID)
	} else {
		m[userID] = max(0, min(200, percent))
	}
	v.Volumes = m
	return v
}

// SoundboardVolume is how loud soundboard sounds are played, in % (0 to 100).
func (v Voice) SoundboardVolume() int { return 100 - max(0, min(100, v.SoundboardCut)) }

// SetVoice saves the voice settings.
func (u *User) SetVoice(v Voice) error {
	u.Profile.Voice = v
	return u.Save()
}

// DefaultSensitivity is the voice activity level when none is chosen (dB).
const DefaultSensitivity = -45.0

// Level is the voice activity level in dB.
func (v Voice) Level() float64 {
	if v.Sensitivity == 0 {
		return DefaultSensitivity
	}
	return v.Sensitivity
}

// Key is the Push-To-Talk key's name (the ` key unless chosen).
func (v Voice) Key() string {
	if v.PTTKey == "" {
		return "`"
	}
	return v.PTTKey
}

// NoiseLevel is the Noise Suppression in use: "off", "standard" or
// "strong" (only when strong is available).
func (v Voice) NoiseLevel(strong bool) string {
	switch {
	case v.Denoise == "off" || (v.Denoise == "" && v.NoDenoise):
		return "off"
	case v.Denoise == "standard" || !strong:
		return "standard"
	}
	return "strong"
}
