package guildstore

// This file keeps what joining needs: the guild's join rules, Rules text, bans, waiting join requests
// and CAPTCHA question bank.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/fsutil"
)

const (
	joinFileName     = "join.json"
	bansFileName     = "bans.json"
	requestsFileName = "requests.json"
	captchaFileName  = "captcha.json"
	captchaImageDir  = "captcha" // The folder is inside media/.

	MaxRules          = 10000
	MaxAllowed        = 1000
	MaxQuestions      = 16 // A bank holds at most 16 questions.
	MaxAsked          = 10 // At most 10 questions are asked.
	MaxTextAnswers    = 4  // A text question has at most 4 answers.
	MaxOptions        = 10
	MaxQuestionText   = 300
	MaxOptionText     = 100
	MaxCaptchaMessage = 1000
	MaxCaptchaImage   = 1024 * 1024
)

// Where the Join button is shown.
const (
	JoinOnAbout           = "about"
	JoinOnRules           = "rules"
	JoinOnAboutAfterRules = "about_after_rules"
)

// These are the question types.
const (
	QDropdown = "dropdown"
	QText     = "text"
	QCheckbox = "checkbox"
	QRadio    = "radio"
)

// JoinSettings is join.json: the owner's join rules.
type JoinSettings struct {
	ShowJoinButton  bool     `json:"show_join_button"`  // It shows the Join button.
	ShowRulesButton bool     `json:"show_rules_button"` // It shows the Rules button.
	Whitelist       bool     `json:"whitelist"`         // It makes the guild invite-only.
	Allowed         []string `json:"allowed"`           // These are the user IDs on the whitelist.
	PasswordHash    string   `json:"password_hash"`     // It is "" when there is no join password.
	MinAccountDays  int      `json:"min_account_days"`  // It is the minimum account age.
	// It is the minimum number of other guilds.
	MinServerCount int    `json:"min_server_count"`
	RulesEnabled   bool   `json:"rules_enabled"` // It turns the Rules page on.
	Rules          string `json:"rules"`         // It is the Rules text.
	RulesPublic    bool   `json:"rules_public"`  // It lets visitors read the Rules.
	RequireRules   bool   `json:"require_rules"` // Joining needs ticking "I agree".
	// It is when the Rules text last changed, in RFC 3339.
	RulesUpdated    string `json:"rules_updated,omitempty"`
	JoinPlacement   string `json:"join_placement"`   // It is where the Join button is shown.
	RequireApproval bool   `json:"require_approval"` // It makes the owner approve each join.
	// It is the Waiting Period.
	PostWaitMinutes int `json:"post_wait_minutes"`
}

// DefaultJoinSettings lets anyone with the address join, with Join on the About page.
func DefaultJoinSettings() JoinSettings {
	return JoinSettings{ShowJoinButton: true, ShowRulesButton: true, JoinPlacement: JoinOnAbout}
}

// Check validates the settings (and tidies the whitelist).
func (s *JoinSettings) Check() error {
	switch s.JoinPlacement {
	case JoinOnAbout, JoinOnRules, JoinOnAboutAfterRules:
	default:
		return errors.New("unknown place for the Join button")
	}
	if s.JoinPlacement != JoinOnAbout && !s.RulesEnabled {
		return errors.New("the Join button can only be on the Rules page when the Rules page is on")
	}
	if utf8.RuneCountInString(s.Rules) > MaxRules {
		return fmt.Errorf("the rules can be at most %d characters", MaxRules)
	}
	if s.MinAccountDays < 0 || s.MinAccountDays > 3650 || s.MinServerCount < 0 || s.MinServerCount > 100 ||
		s.PostWaitMinutes < 0 || s.PostWaitMinutes > 60*24*30 {
		return errors.New("a number in the join rules is out of range")
	}
	var clean []string
	seen := map[string]bool{}
	for _, id := range s.Allowed {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id == "" || seen[id] {
			continue
		}
		if len(id) != 52 {
			return fmt.Errorf("%q is not a user ID (they are 52 letters and digits)", id)
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) > MaxAllowed {
		return fmt.Errorf("the whitelist can hold at most %d user IDs", MaxAllowed)
	}
	s.Allowed = clean
	return nil
}

// Ban is one entry of bans.json.
type Ban struct {
	UserKey string    `json:"user_key"`
	Name    string    `json:"name,omitempty"`
	IP      string    `json:"ip,omitempty"` // It is set when the address is blocked too.
	At      time.Time `json:"at"`
}

// JoinRequest is one entry of requests.json.
type JoinRequest struct {
	UserKey string    `json:"user_key"`
	Name    string    `json:"name,omitempty"`
	IP      string    `json:"ip,omitempty"`
	At      time.Time `json:"at"`
	Created time.Time `json:"created,omitzero"` // It is the account's creation date, as its app said.
	Guilds  int       `json:"guilds,omitempty"` // It is how many guilds the account was in, as its app said.
}

// Captcha is captcha.json: the question bank and its settings.
type Captcha struct {
	Enabled     bool       `json:"enabled"` // It is "Enable CAPTCHA".
	AskCount    int        `json:"ask_count"`
	PassPercent int        `json:"pass_percent"`
	Welcome     string     `json:"welcome"`
	PassMessage string     `json:"pass_message"`
	FailMessage string     `json:"fail_message"`
	Questions   []Question `json:"questions"`
}

// Question is one CAPTCHA question.
type Question struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Text          string   `json:"text"`
	ImageHash     string   `json:"image_hash,omitempty"`
	ImageAlt      string   `json:"image_alt,omitempty"` // It describes the picture in words.
	Options       []string `json:"options,omitempty"`   // These are the choices of a choice question.
	Correct       []int    `json:"correct,omitempty"`   // These are indexes into Options.
	Answers       []string `json:"answers,omitempty"`   // These are a text question's answers, up to 4.
	CaseSensitive bool     `json:"case_sensitive,omitempty"`
	Weight        string   `json:"weight,omitempty"`     // It is "" (Regular), WeightMust or WeightNone.
	AlwaysAsk     bool     `json:"always_ask,omitempty"` // The question is asked every time.
}

// Question weights: a Must question has to be right to pass; a None
// question is asked but not counted.
const (
	WeightMust = "must"
	WeightNone = "none"
)

// DefaultCaptcha is off, asks 3 questions and needs 100% to pass.
func DefaultCaptcha() Captcha { return Captcha{AskCount: 3, PassPercent: 100} }

// CheckSettings validates the CAPTCHA's settings (not its questions).
func (c *Captcha) CheckSettings() error {
	if c.AskCount < 1 || c.AskCount > MaxAsked {
		return fmt.Errorf("ask 1 to %d questions", MaxAsked)
	}
	if c.PassPercent < 0 || c.PassPercent > 100 {
		return errors.New("the pass mark is a percentage from 0 to 100")
	}
	for _, m := range []string{c.Welcome, c.PassMessage, c.FailMessage} {
		if utf8.RuneCountInString(m) > MaxCaptchaMessage {
			return fmt.Errorf("messages can be at most %d characters", MaxCaptchaMessage)
		}
	}
	return nil
}

// Check validates one question.
func (q *Question) Check() error {
	if q.Weight != "" && q.Weight != WeightMust && q.Weight != WeightNone {
		return errors.New("unknown question weight")
	}
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" && q.ImageHash == "" {
		return errors.New("a question needs text or a picture")
	}
	if utf8.RuneCountInString(q.Text) > MaxQuestionText || utf8.RuneCountInString(q.ImageAlt) > MaxQuestionText {
		return fmt.Errorf("question text can be at most %d characters", MaxQuestionText)
	}
	switch q.Type {
	case QText:
		q.Options, q.Correct = nil, nil
		var answers []string
		for _, a := range q.Answers {
			if a != "" { // Answers must match exactly: spaces are kept.
				answers = append(answers, a)
			}
		}
		if len(answers) == 0 || len(answers) > MaxTextAnswers {
			return fmt.Errorf("a text question needs 1 to %d accepted answers", MaxTextAnswers)
		}
		for _, a := range answers {
			if utf8.RuneCountInString(a) > MaxOptionText {
				return fmt.Errorf("answers can be at most %d characters", MaxOptionText)
			}
		}
		q.Answers = answers
	case QDropdown, QRadio, QCheckbox:
		q.Answers, q.CaseSensitive = nil, false
		if len(q.Options) < 2 || len(q.Options) > MaxOptions {
			return fmt.Errorf("a choice question needs 2 to %d choices", MaxOptions)
		}
		for i, o := range q.Options {
			q.Options[i] = strings.TrimSpace(o)
			if q.Options[i] == "" || utf8.RuneCountInString(q.Options[i]) > MaxOptionText {
				return fmt.Errorf("each choice needs text (at most %d characters)", MaxOptionText)
			}
		}
		seen := map[int]bool{}
		for _, c := range q.Correct {
			if c < 0 || c >= len(q.Options) || seen[c] {
				return errors.New("a correct choice is not one of the choices")
			}
			seen[c] = true
		}
		if len(q.Correct) == 0 {
			return errors.New("mark at least one correct choice")
		}
		if q.Type != QCheckbox && len(q.Correct) != 1 {
			return errors.New("this kind of question has exactly one correct choice")
		}
	default:
		return errors.New("unknown question type")
	}
	return nil
}

// ---- loading and saving -------------------------------------------------------

func loadJSONFile(dataDir, name string, v any) error {
	data, err := os.ReadFile(filepath.Join(dataDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s is damaged", name)
	}
	return nil
}

func saveJSONFile(dataDir, name string, v any) error {
	data, _ := json.MarshalIndent(v, "", "  ")
	return fsutil.WriteFileAtomic(filepath.Join(dataDir, name), append(data, '\n'), 0o600)
}

// LoadJoinSettings reads join.json (defaults if missing).
func LoadJoinSettings(dataDir string) (JoinSettings, error) {
	s := DefaultJoinSettings()
	err := loadJSONFile(dataDir, joinFileName, &s)
	return s, err
}

// SaveJoinSettings writes join.json.
func SaveJoinSettings(dataDir string, s JoinSettings) error {
	return saveJSONFile(dataDir, joinFileName, s)
}

// LoadBans reads bans.json.
func LoadBans(dataDir string) ([]Ban, error) {
	var b []Ban
	return b, loadJSONFile(dataDir, bansFileName, &b)
}

// SaveBans writes bans.json.
func SaveBans(dataDir string, b []Ban) error { return saveJSONFile(dataDir, bansFileName, b) }

// LoadRequests reads requests.json.
func LoadRequests(dataDir string) ([]JoinRequest, error) {
	var r []JoinRequest
	return r, loadJSONFile(dataDir, requestsFileName, &r)
}

// SaveRequests writes requests.json.
func SaveRequests(dataDir string, r []JoinRequest) error {
	return saveJSONFile(dataDir, requestsFileName, r)
}

// LoadCaptcha reads captcha.json (defaults if missing).
func LoadCaptcha(dataDir string) (Captcha, error) {
	c := DefaultCaptcha()
	err := loadJSONFile(dataDir, captchaFileName, &c)
	return c, err
}

// SaveCaptcha writes captcha.json.
func SaveCaptcha(dataDir string, c Captcha) error { return saveJSONFile(dataDir, captchaFileName, c) }

func captchaImagePath(dataDir, id string) string {
	return filepath.Join(dataDir, "media", captchaImageDir, id)
}

// SaveCaptchaImage stores a question's picture (nil removes it).
func SaveCaptchaImage(dataDir, id string, img []byte) error {
	path := captchaImagePath(dataDir, id)
	if img == nil {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, img, 0o600)
}

// LoadCaptchaImage reads a question's picture.
func LoadCaptchaImage(dataDir, id string) ([]byte, error) {
	return os.ReadFile(captchaImagePath(dataDir, id))
}

// CheckCaptchaImage makes sure a question picture is a PNG/JPG of at most
// 800×800 and 1 MB (the app shrinks pictures before sending).
func CheckCaptchaImage(img []byte) error {
	if len(img) > MaxCaptchaImage {
		return errors.New("the picture is too large")
	}
	return checkImage(img, 800)
}
