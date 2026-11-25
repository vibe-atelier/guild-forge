package proto

// This file has the messages sent inside an encrypted Conn. Every message has a "type".
//
// A client request carries an ID; the guild's answer carries the same ID.
// The guild can also send "event" messages at any time (ID 0), for example
// when the guild's name changes (live connection).

// Request is sent by the client.
type Request struct {
	ID   uint64 `json:"id,omitempty"`
	Type string `json:"type"`

	// For "pair" and "auth", these are the user's Ed25519 public key and a
	// signature proving the user owns it, tied to this connection.
	OTP     string `json:"otp,omitempty"`
	UserKey []byte `json:"user_key,omitempty"`
	Sig     []byte `json:"sig,omitempty"`

	// "auth": which computer this is, and whether to take the guild
	// over from another computer ("Use here").
	DeviceID string `json:"device_id,omitempty"`
	TakeOver bool   `json:"take_over,omitempty"`

	// "about": the picture the client already has, so it is not sent again.
	PictureHash string `json:"picture_hash,omitempty"`

	// These are for "update_guild" (owner only).
	Name            *string `json:"name,omitempty"`
	About           *string `json:"about,omitempty"`
	Picture         []byte  `json:"picture,omitempty"`
	ClearPicture    bool    `json:"clear_picture,omitempty"`
	ShowMemberCount *bool   `json:"show_member_count,omitempty"`

	// For "transfer_offer", it is the member (user key text) to offer it to.
	Target string `json:"target,omitempty"`

	// "auth" and "join": the name the user goes by.
	UserName string `json:"user_name,omitempty"`

	// "join": what the guild's join rules check.
	Created      string `json:"created,omitempty"` // It is the account's creation date, in RFC 3339.
	ServerCount  int    `json:"server_count,omitempty"`
	JoinPassword string `json:"join_password,omitempty"`
	AgreedRules  bool   `json:"agreed_rules,omitempty"`

	// "join_answers": one answer per CAPTCHA question, in order.
	Answers []QuizAnswer `json:"answers,omitempty"`

	// These are the owner's requests: "update_join", "captcha_settings", "captcha_put" (+ Image or
	// ClearImage), "captcha_delete" / "captcha_image" (QuestionID),
	// "ban" (Target, BanIP), "unban" / "kick" / "approve" / "decline" (Target).
	Join       *JoinSettings    `json:"join,omitempty"`
	Captcha    *CaptchaSettings `json:"captcha,omitempty"`
	Question   *CaptchaQuestion `json:"question,omitempty"`
	QuestionID string           `json:"question_id,omitempty"`
	Image      []byte           `json:"image,omitempty"`
	ClearImage bool             `json:"clear_image,omitempty"`
	BanIP      bool             `json:"ban_ip,omitempty"`
	// For "timeout", it is how long, in minutes (0 lifts it).
	Minutes int `json:"minutes,omitempty"`

	// These are for voice: "voice_join" (ChannelID), "voice_leave", "voice_state"
	// (Muted, Deafened), and "voice" (Voice: one packet, never answered).
	Voice *VoicePacket `json:"voice,omitempty"`
	// It carries "stream"; "stream_start", "stream_stop", "stream_watch" and
	// "stream_unwatch" use Target, and "camera_start" and "camera_stop" need
	// nothing.
	Stream   *StreamPacket `json:"stream,omitempty"`
	Muted    bool          `json:"muted,omitempty"`
	Deafened bool          `json:"deafened,omitempty"`
	// These are for voice moderation: "voice_mute" / "voice_deafen" (Target, On), "voice_move"
	// (Target, ChannelID), "voice_disconnect" (Target), "voice_lock"
	// (ChannelID, On); "sound_list", "sound_put" (Sound, SoundData, and
	// Image for its picture),
	// "sound_delete", "sound_get" and "sound_play" (SoundID).
	Sound     *Sound `json:"sound,omitempty"`
	SoundData []byte `json:"sound_data,omitempty"`
	SoundID   string `json:"sound_id,omitempty"`

	// These are for channels and messages.
	// For "auth" and "join", it is the user's X25519 public key.
	EncKey []byte `json:"enc_key,omitempty"`
	// For "auth", it is the user's custom typing text.
	TypingText string       `json:"typing_text,omitempty"`
	Node       *ChannelNode `json:"node,omitempty"`       // It is used by "channel_put".
	ChannelID  string       `json:"channel_id,omitempty"` // It is used by the channel requests.
	// For "channel_move", it is the new parent ("" means the top).
	Under string `json:"under,omitempty"`
	// For "channel_move", it is the place there (0 is first).
	Place     int          `json:"place,omitempty"`
	Boxes     []KeyBox     `json:"boxes,omitempty"`      // It is used by "put_keys".
	Msg       *ChatMessage `json:"msg,omitempty"`        // It is used by "send" and "edit".
	MessageID string       `json:"message_id,omitempty"` // It is used by "edit", "delete" and "react".
	// For "history", it asks for the messages before this ID.
	Before string `json:"before,omitempty"`
	// For "schedule", it is when to post (RFC 3339).
	At string `json:"at,omitempty"`
	// For "poll_vote", it is the choice (from 0), or -1 to take a vote back.
	Choice int    `json:"choice,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Emoji  string `json:"emoji,omitempty"` // It is used by "react".
	// For "react", it means add (true) or remove; voice moderation uses it too.
	On bool `json:"on,omitempty"`

	// These are for roles.
	Role   *Role    `json:"role,omitempty"`    // It is used by "role_put".
	RoleID string   `json:"role_id,omitempty"` // It is used by "role_delete".
	Roles  []string `json:"roles,omitempty"`   // For "member_roles", it is the target's new role IDs.

	// These are for private messages and status.
	// For "auth" and "set_status", it is Online, Idle, DND or Invisible.
	Status string      `json:"status,omitempty"`
	DM     *DMEnvelope `json:"dm,omitempty"`     // It is used by "dm_send".
	DMIDs  []string    `json:"dm_ids,omitempty"` // For "dm_ack", it lists the envelopes the app has saved.

	// These are for guild pages.
	Pages   *PageSettings `json:"pages,omitempty"`   // It is used by "update_pages".
	Event   *Event        `json:"event,omitempty"`   // It is used by "event_put".
	Wiki    *WikiPage     `json:"wiki,omitempty"`    // It is used by "wiki_put".
	Post    *Post         `json:"post,omitempty"`    // It is used by "post_put".
	Comment *Comment      `json:"comment,omitempty"` // It is used by "comment_put".
	ItemID  string        `json:"item_id,omitempty"` // It is an event, Wiki page, post or comment ID.
	// For "vote", it is 1 for like, -1 for dislike and 0 for none.
	Vote int `json:"vote,omitempty"`
	// It is used by "emoji_put", "emoji_delete" and "emoji_borrow".
	EmojiName string `json:"emoji_name,omitempty"`
	Hash      string `json:"hash,omitempty"` // It is used by "image_get" and "emoji_get".

	// These are for profiles, nicknames and synced settings.
	Profile *MemberProfile `json:"profile,omitempty"` // It is used by "set_profile".
	// For "set_profile", it is the banner (Picture is the picture).
	Banner   []byte          `json:"banner,omitempty"`
	Nick     *string         `json:"nick,omitempty"`     // For "set_nick", "" removes the nickname.
	Settings *MemberSettings `json:"settings,omitempty"` // It is used by "settings_put".
	// For "update_guild", it is the guild's default.
	DefaultNotify *string `json:"default_notify,omitempty"`

	// These are for the currency.
	Currency *CurrencySettings `json:"currency,omitempty"`  // It is used by "currency_update".
	Amount   int64             `json:"amount,omitempty"`    // It is used by "mint", "give" and "transfer".
	ShopItem *ShopItem         `json:"shop_item,omitempty"` // It is used by "shop_put".
}

// Response is sent by the guild: an answer (same ID as the request) or an
// event (Type "event").
type Response struct {
	ID    uint64 `json:"id,omitempty"`
	Type  string `json:"type"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Voice  *VoicePacket  `json:"voice,omitempty"`  // For "voice", it is a packet from someone.
	Stream *StreamPacket `json:"stream,omitempty"` // For "stream", it is part of a shared screen.
	// Code is a machine-readable reason, e.g. "elsewhere", "not_member".
	Code string `json:"code,omitempty"`

	// These answer "info".
	HasOwner     bool `json:"has_owner,omitempty"`
	Broadcasting bool `json:"broadcasting,omitempty"`

	// These answer "pair_result" when the OTP was wrong.
	TriesLeft int `json:"tries_left,omitempty"`

	// These answer "auth".
	IsOwner bool `json:"is_owner,omitempty"`
	// For "auth", it lists where else this guild answers.
	Addresses []string      `json:"addresses,omitempty"`
	Transfer  *TransferInfo `json:"transfer,omitempty"`

	// This answers "about".
	Guild *GuildInfo `json:"guild,omitempty"`

	// This answers "members".
	Members []MemberInfo `json:"members,omitempty"`

	// For "rules", it is the Rules text.
	Rules string `json:"rules,omitempty"`

	// "join" / "join_answers": a CAPTCHA to answer (Code "captcha"), the
	// owner's pass/fail message, or the guild-signed receipt when joined.
	Quiz    *Quiz        `json:"quiz,omitempty"`
	Message string       `json:"message,omitempty"`
	Receipt *JoinReceipt `json:"receipt,omitempty"`

	// These answer the owner's "join_settings", "captcha_get", "captcha_image", "requests" and "bans".
	JoinSettings *JoinSettings     `json:"join_settings,omitempty"`
	CaptchaFull  *CaptchaSettings  `json:"captcha_full,omitempty"`
	Questions    []CaptchaQuestion `json:"questions,omitempty"`
	Image        []byte            `json:"image,omitempty"`
	People       []PersonInfo      `json:"people,omitempty"`

	// These are for channels and messages.
	Channels []ChannelNode `json:"channels,omitempty"`
	Keys     []KeyState    `json:"keys,omitempty"`
	Roles    []Role        `json:"roles,omitempty"` // It answers "roles".
	// For "roles", the key "" means guild-wide; the others are channel IDs.
	MyPerms   map[string][]string `json:"my_perms,omitempty"`
	DMs       []DMEnvelope        `json:"dms,omitempty"` // It answers "dm_inbox".
	DM        *DMEnvelope         `json:"dm,omitempty"`  // It carries the event "dm".
	Pages     *PageSettings       `json:"pages,omitempty"`
	Events    []Event             `json:"events,omitempty"`
	Wikis     []WikiPage          `json:"wikis,omitempty"`
	Wiki      *WikiPage           `json:"wiki,omitempty"`
	Posts     []Post              `json:"posts,omitempty"`
	Post      *Post               `json:"post,omitempty"`
	Comments  []Comment           `json:"comments,omitempty"`
	Emojis    []Emoji             `json:"emojis,omitempty"`
	Data      []byte              `json:"data,omitempty"`     // It answers "image_get" and "emoji_get".
	Settings  *MemberSettings     `json:"settings,omitempty"` // It answers "settings_get".
	Currency  *CurrencySettings   `json:"currency,omitempty"` // It answers "currency".
	Wallet    *Wallet             `json:"wallet,omitempty"`   // It answers "wallet".
	Balances  map[string]int64    `json:"balances,omitempty"` // It answers "balances", for staff only.
	Ledger    []LedgerEntry       `json:"ledger,omitempty"`
	ShopItems []ShopItem          `json:"shop_items,omitempty"`
	Boxes     []KeyBox            `json:"boxes,omitempty"`
	Messages  []ChatMessage       `json:"messages,omitempty"`
	Chat      *ChatMessage        `json:"chat,omitempty"` // It carries the event "message".
	// It is used by the events "typing" and "keys_needed".
	Channel string `json:"channel,omitempty"`
	// It is used by the events "typing" and "sound_play".
	User    string  `json:"user,omitempty"`
	Sounds  []Sound `json:"sounds,omitempty"`   // It answers "sound_list".
	SoundID string  `json:"sound_id,omitempty"` // It is used by the event "sound_play".
	// For "history", it is true when older messages exist.
	More bool `json:"more,omitempty"`
	// For "scheduled", it lists your scheduled messages.
	Scheduled []Scheduled `json:"scheduled,omitempty"`

	// "event": what happened: "guild_updated", "owner_changed",
	// "transfer_changed", "taken_over" (another computer pressed Use here),
	// "removed" (kicked or banned), "requests_changed" (owner: join requests),
	// "approved" / "declined" (to a person waiting for approval).
	Event string `json:"event,omitempty"`
}

// GuildInfo is the public About information.
type GuildInfo struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	About           string        `json:"about"`
	MemberCount     int           `json:"member_count"` // It is -1 when the owner hides it.
	ShowMemberCount bool          `json:"show_member_count"`
	OwnerKey        string        `json:"owner_key,omitempty"`
	PictureHash     string        `json:"picture_hash,omitempty"`
	Picture         []byte        `json:"picture,omitempty"` // It is sent only if the client's hash differs.
	Join            *JoinInfo     `json:"join,omitempty"`
	Pages           *PageSettings `json:"pages,omitempty"` // It says which pages are on.
	// It is "all" or "mentions".
	DefaultNotify string `json:"default_notify,omitempty"`
	// It is set while the currency is on; the bank and pool are only for staff.
	Currency *CurrencySettings `json:"currency,omitempty"`
}

// MemberInfo is one member of a guild.
type MemberInfo struct {
	UserKey string `json:"user_key"`
	Name    string `json:"name,omitempty"`
	// It is the X25519 public key, for sealing channel keys.
	EncKey []byte   `json:"enc_key,omitempty"`
	Typing string   `json:"typing,omitempty"` // It is their custom "is typing" text.
	Roles  []string `json:"roles,omitempty"`  // These are role IDs.
	// It is Online, Idle or DND while they are online.
	Status string `json:"status,omitempty"`
	// It is true when they joined by ticking "I agree".
	AgreedRules bool   `json:"agreed_rules,omitempty"`
	Nick        string `json:"nick,omitempty"` // It is their name in this guild.
	// It is in RFC 3339, while they are timed out.
	TimeoutUntil string `json:"timeout_until,omitempty"`
	Bio          string `json:"bio,omitempty"`          // It is their bio.
	PictureHash  string `json:"picture_hash,omitempty"` // Get the picture with "image_get".
	BannerHash   string `json:"banner_hash,omitempty"`
	Joined       string `json:"joined"` // It is in RFC 3339.
	// It is when the account was created; only staff get it.
	Created string `json:"created,omitempty"`
	Guilds  int    `json:"guilds,omitempty"` // It is how many guilds they are in; only staff get it.
	IsOwner bool   `json:"is_owner,omitempty"`
	Online  bool   `json:"online,omitempty"`
}

// TransferInfo describes a pending ownership transfer.
type TransferInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// JoinInfo tells a visitor what joining needs (part of the public About).
type JoinInfo struct {
	// It is when the Rules last changed, in RFC 3339.
	RulesUpdated    string `json:"rules_updated,omitempty"`
	ShowJoinButton  bool   `json:"show_join_button"`
	ShowRulesButton bool   `json:"show_rules_button"`
	RulesEnabled    bool   `json:"rules_enabled"`
	RulesPublic     bool   `json:"rules_public"`
	RequireRules    bool   `json:"require_rules"`
	JoinPlacement   string `json:"join_placement"`
	NeedsPassword   bool   `json:"needs_password"`
	Whitelist       bool   `json:"whitelist"`
	RequireApproval bool   `json:"require_approval"`
	Captcha         bool   `json:"captcha"`
	MinAccountDays  int    `json:"min_account_days"`
	MinServerCount  int    `json:"min_server_count"`
	PostWaitMinutes int    `json:"post_wait_minutes,omitempty"` // It is the Waiting Period.
}

// JoinSettings are the owner's join rules.
type JoinSettings struct {
	ShowJoinButton  bool     `json:"show_join_button"`
	ShowRulesButton bool     `json:"show_rules_button"`
	Whitelist       bool     `json:"whitelist"`
	Allowed         []string `json:"allowed"`
	HasPassword     bool     `json:"has_password"`       // The guild fills this in.
	Password        *string  `json:"password,omitempty"` // The app sends the new password here; "" removes it.
	MinAccountDays  int      `json:"min_account_days"`
	MinServerCount  int      `json:"min_server_count"`
	RulesEnabled    bool     `json:"rules_enabled"`
	Rules           string   `json:"rules"`
	RulesPublic     bool     `json:"rules_public"`
	RequireRules    bool     `json:"require_rules"`
	JoinPlacement   string   `json:"join_placement"`
	RequireApproval bool     `json:"require_approval"`
	PostWaitMinutes int      `json:"post_wait_minutes"`
}

// CaptchaSettings are the CAPTCHA's settings.
type CaptchaSettings struct {
	Enabled     bool   `json:"enabled"`
	AskCount    int    `json:"ask_count"`
	PassPercent int    `json:"pass_percent"`
	Welcome     string `json:"welcome"`
	PassMessage string `json:"pass_message"`
	FailMessage string `json:"fail_message"`
}

// CaptchaQuestion is a question as the owner edits it (with its answers).
type CaptchaQuestion struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"` // It is dropdown, text, checkbox or radio.
	Text          string   `json:"text"`
	HasImage      bool     `json:"has_image"`
	ImageAlt      string   `json:"image_alt"`
	Options       []string `json:"options"`
	Correct       []int    `json:"correct"`
	Answers       []string `json:"answers"`
	CaseSensitive bool     `json:"case_sensitive"`
	Weight        string   `json:"weight,omitempty"`     // It is "" (Regular), "must" or "none".
	AlwaysAsk     bool     `json:"always_ask,omitempty"` // The question is asked every time.
}

// These are the question weights, the same as guildstore's.
const (
	WeightMust = "must" // The question must be right to pass.
	WeightNone = "none" // The question is asked, but not counted.
)

// Quiz is the CAPTCHA as a joining person sees it: no answers, choices
// shuffled.
type Quiz struct {
	Welcome   string         `json:"welcome"`
	Questions []QuizQuestion `json:"questions"`
}

// QuizQuestion is one question of a Quiz.
type QuizQuestion struct {
	Type     string   `json:"type"`
	Text     string   `json:"text"`
	Image    []byte   `json:"image,omitempty"`
	ImageAlt string   `json:"image_alt,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// QuizAnswer is the answer to one QuizQuestion: chosen option numbers (as
// shown) for choice questions, or the typed text.
type QuizAnswer struct {
	Choices []int  `json:"choices,omitempty"`
	Text    string `json:"text,omitempty"`
}

// JoinReceipt is the guild's signed statement that a user joined.
type JoinReceipt struct {
	GuildID string `json:"guild_id"`
	UserKey string `json:"user_key"`
	Joined  string `json:"joined"` // It is in RFC 3339.
	Sig     []byte `json:"sig"`
}

// ReceiptMessage is what a JoinReceipt's signature covers.
func ReceiptMessage(guildID, userKey, joined string) []byte {
	return []byte("nexus-join-receipt-v1|" + guildID + "|" + userKey + "|" + joined)
}

// PersonInfo is a join request or a ban, for the owner.
type PersonInfo struct {
	UserKey string `json:"user_key"`
	Name    string `json:"name,omitempty"`
	At      string `json:"at"` // It is in RFC 3339.
	IP      string `json:"ip,omitempty"`
	Created string `json:"created,omitempty"` // For join requests, it is when the account was created.
	Guilds  int    `json:"guilds,omitempty"`  // For join requests, it is how many guilds the account is in.
}

// JoinProof is what a user signs to prove who they are when joining.
func JoinProof(sessionID [32]byte) []byte {
	return append([]byte("nexus-join-v1"), sessionID[:]...)
}

// ---- Channels and end-to-end encrypted messages -------------------

// Channel list node kinds.
const (
	KindCategory = "category"
	KindFolder   = "folder"
	KindChannel  = "channel"
	KindVoice    = "voice" // It is a voice channel.
)

// ChannelNode is a category, folder or text channel.
type ChannelNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Order  int    `json:"order"`
	Topic  string `json:"topic,omitempty"`
	// It says whether newcomers read older messages; nil means the parent decides.
	History *bool `json:"history,omitempty"`
	// Messages are deleted after N days, and 0 means never; nil means the parent decides.
	KeepDays *int `json:"keep_days,omitempty"`
	// It is Slow Mode: the seconds between one member's messages.
	Slow int `json:"slow,omitempty"`
	// These are pinned message IDs, newest first (filled in by the guild).
	Pins []string `json:"pins,omitempty"`

	// The guild fills these in: the settings that actually apply.
	EffHistory  bool `json:"eff_history"`
	EffKeepDays int  `json:"eff_keep_days"`

	// These are permission rules for roles. Categories, folders
	// and channels can have their own; InheritPerms uses the parent's instead.
	Overrides    []Override `json:"overrides,omitempty"`
	InheritPerms bool       `json:"inherit_perms,omitempty"`

	// A voice channel: who is in it now (filled in by the guild),
	// and whether it is locked.
	Voice  []VoiceMember `json:"voice,omitempty"`
	Locked bool          `json:"locked,omitempty"`
}

// KeyBox is a channel key sealed for one member.
type KeyBox struct {
	Channel string `json:"channel"`
	Version int    `json:"version"`
	To      string `json:"to"`   // It is the recipient's user key.
	From    string `json:"from"` // It is who made the box.
	// It holds an X25519 ephemeral public key, a nonce and the sealed key, in that order.
	Box []byte `json:"box"`
}

// KeyState is the ledger for one channel.
type KeyState struct {
	Channel  string   `json:"channel"`
	Viewers  []string `json:"viewers,omitempty"` // These are the members who may read the channel.
	Current  int      `json:"current"`           // It is 0 when there is no key yet.
	Retired  bool     `json:"retired"`           // It is true when a new key is needed.
	Missing  []string `json:"missing"`           // These are the members without a box for the current key.
	Holders  int      `json:"holders"`           // It counts the members who have the key.
	History  bool     `json:"history"`           // It says whether newcomers may get older keys.
	Versions []int    `json:"versions"`          // These are the key versions that exist.
}

// ChatMessage is one message, as stored and sent by the guild. Text is only
// ever inside Cipher (encrypted with the channel key).
type ChatMessage struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	Author  string `json:"author"`
	Version int    `json:"version"` // It is the key version used.
	Nonce   []byte `json:"nonce,omitempty"`
	Cipher  []byte `json:"cipher,omitempty"`
	Sig     []byte `json:"sig,omitempty"` // It is the author's signature over SignedPart.
	ReplyTo string `json:"reply_to,omitempty"`
	Time    string `json:"time"` // It is in RFC 3339, set by the guild.
	Edited  string `json:"edited,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
	// It maps each emoji to the user keys that chose it.
	Reactions map[string][]string `json:"reactions,omitempty"`

	// These are for the message extras.
	// The message deletes itself then, in RFC 3339 (Disappearing Messages).
	Expires string    `json:"expires,omitempty"`
	Gone    bool      `json:"gone,omitempty"`    // The message has disappeared, so apps hide it altogether.
	Earlier []Earlier `json:"earlier,omitempty"` // It is the Edit History: the sealed texts before each edit.
	// It marks a poll; its question and choices are in the sealed text.
	Poll  *PollInfo  `json:"poll,omitempty"`
	Votes [][]string `json:"votes,omitempty"` // It lists, per choice, who voted for it.
}

// Earlier is a message's text before an edit, still sealed (Edit History).
type Earlier struct {
	Version int    `json:"version"`
	Nonce   []byte `json:"nonce"`
	Cipher  []byte `json:"cipher"`
	Time    string `json:"time"` // It is when this text was written.
}

// PollInfo is what the guild needs to count a poll's votes: how many
// choices, and whether it is a quiz (one right answer; a vote can't be
// changed).
type PollInfo struct {
	Choices int  `json:"choices"`
	Quiz    bool `json:"quiz,omitempty"`
}

// Scheduled is a message the guild posts later for its writer.
type Scheduled struct {
	ID  string      `json:"id"`
	At  string      `json:"at"` // It is in RFC 3339.
	Msg ChatMessage `json:"msg"`
}

// SignedPart is what the author signs (the guild can't change it unnoticed).
func (m *ChatMessage) SignedPart() []byte {
	b := []byte("nexus-msg-v1|" + m.Channel + "|" + m.Author + "|" + m.ReplyTo + "|")
	b = append(b, byte(m.Version>>24), byte(m.Version>>16), byte(m.Version>>8), byte(m.Version))
	b = append(b, m.Nonce...)
	return append(b, m.Cipher...)
}

// ---- Roles and permissions ----------------------

// Role is a guild role. Rank orders roles: you can only manage people and
// roles ranked strictly below your highest role, so equal ranks can't touch
// each other. The Everyone role (ID "everyone") is always rank 0.
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Rank        int      `json:"rank"`
	Color       string   `json:"color,omitempty"`       // It is "#rrggbb", or "" for none.
	Hoist       bool     `json:"hoist,omitempty"`       // The role is shown as its own group in the member list.
	Mentionable bool     `json:"mentionable,omitempty"` // The role can be @mentioned.
	Perms       []string `json:"perms"`                 // These are the permissions it gives.

	// Bought in the Shop. BuyableBy are the roles that may buy it
	// (none = every member); Exclusive are roles it can't be worn with:
	// equipping one takes the other off.
	Purchasable bool     `json:"purchasable,omitempty"`
	BuyableBy   []string `json:"buyable_by,omitempty"`
	Exclusive   []string `json:"exclusive,omitempty"`
}

// EveryoneRole is the role every member has.
const EveryoneRole = "everyone"

// Override is one role's rule on a category, folder or channel: permissions
// it allows and denies there (anything else is inherited).
type Override struct {
	Role  string   `json:"role"`
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// These are the permissions. Channel permissions can also be set per
// category, folder or channel.
const (
	PermAdministrator   = "administrator"
	PermManageGuild     = "manage_guild"
	PermManageRoles     = "manage_roles"
	PermManageChannels  = "manage_channels"
	PermManageJoining   = "manage_joining"
	PermApproveJoins    = "approve_joins"
	PermKick            = "kick"
	PermBan             = "ban"
	PermTimeout         = "timeout_members" // This lets members time others out.
	PermViewChannel     = "view_channel"
	PermSendMessages    = "send_messages"
	PermAddReactions    = "add_reactions"
	PermManageMessages  = "manage_messages"
	PermPinMessages     = "pin_messages"
	PermConnect         = "connect"
	PermSpeak           = "speak"
	PermStream          = "stream"
	PermPrioritySpeaker = "priority_speaker"
	// Voice moderation has one permission for each action.
	PermMuteMembers       = "mute_members"
	PermDeafenMembers     = "deafen_members"
	PermMoveMembers       = "move_members"
	PermDisconnectMembers = "disconnect_members"
	PermLockChannel       = "lock_channel"
	PermUseSoundboard     = "use_soundboard"
	PermManageSoundboard  = "manage_soundboard" // This is guild-wide.
	PermPostThreads       = "post_threads"
	PermEditWiki          = "edit_wiki"
	PermManageEvents      = "manage_events"
	PermManagePages       = "manage_pages"
	PermChangeNickname    = "change_nickname"  // This is for your own nickname.
	PermManageNicknames   = "manage_nicknames" // This is for other people's nicknames.
	PermManageCurrency    = "manage_currency"
)

// GuildPerms are permissions that only apply to the whole guild.
var GuildPerms = []string{PermAdministrator, PermManageGuild, PermManageRoles, PermManageChannels,
	PermManageJoining, PermApproveJoins, PermKick, PermBan, PermTimeout,
	PermPostThreads, PermEditWiki, PermManageEvents, PermManagePages, PermChangeNickname, PermManageNicknames, PermManageCurrency,
	PermManageSoundboard}

// ChannelPerms can be changed per category, folder or channel.
var ChannelPerms = []string{PermViewChannel, PermSendMessages, PermAddReactions, PermManageMessages, PermPinMessages,
	PermConnect, PermSpeak, PermStream, PermPrioritySpeaker, PermMuteMembers, PermDeafenMembers,
	PermMoveMembers, PermDisconnectMembers, PermLockChannel, PermUseSoundboard}

// VoiceModPerms replace the old "moderate_voice" permission.
var VoiceModPerms = []string{PermMuteMembers, PermDeafenMembers, PermMoveMembers, PermDisconnectMembers, PermLockChannel}

// EveryoneDefaults are the Everyone role's permissions in a new guild.
var EveryoneDefaults = []string{PermViewChannel, PermSendMessages, PermAddReactions, PermConnect, PermSpeak, PermStream,
	PermUseSoundboard, PermPostThreads, PermChangeNickname}

// IsPerm reports whether p is a known permission.
func IsPerm(p string) bool {
	for _, list := range [][]string{GuildPerms, ChannelPerms} {
		for _, q := range list {
			if q == p {
				return true
			}
		}
	}
	return false
}

// ---- Private messages ----------------

// DMEnvelope is one private message passed between two members through a
// guild they share. The guild sees who it is from and to, but the content is
// encrypted for the two of them and signed by the sender.
type DMEnvelope struct {
	ID     string `json:"id"`     // It is set by the guild.
	From   string `json:"from"`   // It is the sender's user key.
	To     string `json:"to"`     // It is the recipient's user key.
	Time   string `json:"time"`   // It is in RFC 3339, set by the guild.
	Live   bool   `json:"live"`   // It is only for someone online now (typing), and is never stored.
	Nonce  []byte `json:"nonce"`  // It is the AES-GCM nonce.
	Cipher []byte `json:"cipher"` // It is the encrypted content.
	Sig    []byte `json:"sig"`    // It is the sender's signature over SignedPart.
}

// SignedPart is what the sender signs.
func (d *DMEnvelope) SignedPart() []byte {
	live := "0"
	if d.Live {
		live = "1"
	}
	b := []byte("nexus-dm-v1|" + d.From + "|" + d.To + "|" + live + "|")
	b = append(b, d.Nonce...)
	b = append(b, '|')
	return append(b, d.Cipher...)
}

// These are the user statuses.
var Statuses = []string{"Online", "Idle", "DND", "Invisible"}

// ---- Guild pages ---------------------
// These pages are readable by the guild's host.

// PageSettings say which pages a guild shows.
type PageSettings struct {
	Calendar bool `json:"calendar"`
	Threads  bool `json:"threads"`
	Wiki     bool `json:"wiki"`
}

// Event is a Calendar event. Times are RFC 3339 in UTC; the zones are
// the time zones they were written in (shown with the times).
type Event struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Start     string `json:"start"`                // It is the start (a date, or a date and time).
	StartTime bool   `json:"start_time,omitempty"` // It is true when Start has a time of day.
	StartZone string `json:"start_zone,omitempty"`
	End       string `json:"end,omitempty"` // It is optional.
	EndTime   bool   `json:"end_time,omitempty"`
	EndZone   string `json:"end_zone,omitempty"`
	Details   string `json:"details,omitempty"` // It is in Markdown.
	Author    string `json:"author,omitempty"`
	Updated   string `json:"updated,omitempty"`
}

// WikiPage is one Wiki topic.
type WikiPage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Group   string `json:"group,omitempty"` // It is a heading in the Wiki list.
	Body    string `json:"body,omitempty"`  // It is in Markdown, and left out in lists.
	Author  string `json:"author,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// Post is a Threads post.
type Post struct {
	ID       string         `json:"id"`
	Author   string         `json:"author,omitempty"`
	Name     string         `json:"name,omitempty"` // It is the author's name when listed.
	Title    string         `json:"title"`
	Body     string         `json:"body,omitempty"` // It is in Markdown.
	Time     string         `json:"time,omitempty"`
	Edited   bool           `json:"edited,omitempty"`
	Deleted  bool           `json:"deleted,omitempty"`
	Votes    map[string]int `json:"votes,omitempty"` // The guild keeps these; they are not sent.
	Likes    int            `json:"likes,omitempty"`
	Dislikes int            `json:"dislikes,omitempty"`
	MyVote   int            `json:"my_vote,omitempty"`
	Comments int            `json:"comments,omitempty"`
}

// Comment is a comment on a post; Parent is the comment it replies to.
type Comment struct {
	ID       string         `json:"id"`
	Post     string         `json:"post"`
	Parent   string         `json:"parent,omitempty"`
	Author   string         `json:"author,omitempty"`
	Name     string         `json:"name,omitempty"`
	Body     string         `json:"body"`
	Time     string         `json:"time,omitempty"`
	Edited   bool           `json:"edited,omitempty"`
	Deleted  bool           `json:"deleted,omitempty"`
	Votes    map[string]int `json:"votes,omitempty"`
	Likes    int            `json:"likes,omitempty"`
	Dislikes int            `json:"dislikes,omitempty"`
	MyVote   int            `json:"my_vote,omitempty"`
}

// Emoji is a custom emoji: a name like "pixel_heart" and its picture.
type Emoji struct {
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	Borrowed bool   `json:"borrowed,omitempty"` // It is a copy of another guild's emoji.
	From     string `json:"from,omitempty"`     // It is that guild's ID.
}

// ---- Profiles and synced settings -------------------

// MemberProfile is what a member shows others besides their name.
// The pictures travel in the request's Picture and Banner fields.
type MemberProfile struct {
	Name         string `json:"name,omitempty"` // It is your name, when it has changed.
	Bio          string `json:"bio"`
	ClearPicture bool   `json:"clear_picture,omitempty"`
	ClearBanner  bool   `json:"clear_banner,omitempty"`
}

// These are the notification choices.
const (
	NotifyAll      = "all"
	NotifyMentions = "mentions"
	NotifyNothing  = "nothing"
)

// MemberSettings are a member's settings that the guild keeps for them, so
// they come back on any computer.
type MemberSettings struct {
	// It maps a channel ID, or "guild", to NotifyAll, NotifyMentions or NotifyNothing.
	Notify map[string]string `json:"notify,omitempty"`
	// It maps each channel ID to the last read message ID.
	Reads     map[string]string `json:"reads,omitempty"`
	Collapsed []string          `json:"collapsed,omitempty"` // These are the folded category IDs.
}

// ---- Guild currency, wallet and shop ---------------

// CurrencySettings is a guild's currency. Bank and Pool are only filled in
// for people with Manage Currency.
type CurrencySettings struct {
	Enabled  bool   `json:"enabled"`
	Name     string `json:"currency_name"`           // It is "Coins" if empty.
	IconHash string `json:"currency_icon,omitempty"` // It is a picture ("image_get"); 🪙 is used if empty.
	Shop     bool   `json:"enable_shop"`
	Bank     int64  `json:"bank"` // These coins are made but not given out yet.
	Pool     int64  `json:"pool"` // These coins are held by members.
}

// LedgerEntry is one movement of coins.
type LedgerEntry struct {
	ID     string `json:"id"`
	Time   string `json:"time"`
	Kind   string `json:"kind"`           // It is "mint", "give", "transfer", "buy", "bid" or "refund".
	From   string `json:"from,omitempty"` // It is a user key, or "" for the bank.
	To     string `json:"to,omitempty"`   // It is a user key, or "" for the bank.
	Amount int64  `json:"amount"`
	By     string `json:"by,omitempty"`   // It is who did it (for mint and give).
	Item   string `json:"item,omitempty"` // For "buy", it is the item's name.
}

// Wallet is what a member sees of their own coins.
type Wallet struct {
	Balance int64         `json:"balance"`
	History []LedgerEntry `json:"history"` // The history is newest first.
}

// ShopItem is something for sale in the Shop.
type ShopItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"` // It is in Markdown.
	Price       int64  `json:"price"`
	ImageHash   string `json:"image_hash,omitempty"`
	Type        string `json:"type,omitempty"`    // It is ShopRole or ShopOther ("" means other).
	RoleID      string `json:"role_id,omitempty"` // For ShopRole, it is the role it gives.

	// Sale Type: SaleMarket (bought at Price) or SaleAuction (Price
	// is the starting bid). The guild keeps the bidding.
	Sale   string `json:"sale,omitempty"`
	Ends   string `json:"ends,omitempty"`   // For auctions, it is the end, in RFC 3339.
	Bid    int64  `json:"bid,omitempty"`    // It is the highest bid, held from the bidder.
	Bidder string `json:"bidder,omitempty"` // It is the bidder's user key.
	Ended  bool   `json:"ended,omitempty"`  // The auction is over: the Bidder (if any) won it.

	// Filled in for the member who asked.
	Owned    bool `json:"owned,omitempty"`    // It is true for a role they bought.
	Equipped bool `json:"equipped,omitempty"` // It is true when they are wearing it now.
	Locked   bool `json:"locked,omitempty"`   // It is true when the item is not for their roles.
}

// These are the kinds of Shop item.
const (
	ShopRole  = "role"
	ShopOther = "other"
)

// These are the Sale Types.
const (
	SaleMarket  = "market"
	SaleAuction = "auction"
)

// ---- Voice -----------------------------------------------

// VoiceMember is someone in a voice channel.
type VoiceMember struct {
	User     string `json:"user"`
	Muted    bool   `json:"muted,omitempty"`
	Deafened bool   `json:"deafened,omitempty"`
	Live     bool   `json:"live,omitempty"`   // It is true while they share their screen.
	Camera   bool   `json:"camera,omitempty"` // It is true while their camera is on.
	// Set by a moderator; only a moderator can lift them.
	GuildMuted    bool `json:"guild_muted,omitempty"`
	GuildDeafened bool `json:"guild_deafened,omitempty"`
}

// VoicePacket is 20 ms of someone's voice: Opus, sealed with the voice
// channel's key (like a message), so the guild passes it on without being
// able to hear it.
type VoicePacket struct {
	Channel string `json:"channel"`
	From    string `json:"from,omitempty"` // It is filled in by the guild.
	Version int    `json:"version"`        // It is the channel key's version.
	Seq     uint32 `json:"seq"`            // It counts up, to spot lost packets.
	// Priority means the Priority Speaker key is held. The guild clears
	// it for those without the permission.
	Priority bool   `json:"priority,omitempty"`
	Nonce    []byte `json:"nonce"`
	Cipher   []byte `json:"cipher"`
}

// IsRoom reports whether kind is a channel or a voice channel, which have keys and members (as
// opposed to categories and folders).
func IsRoom(kind string) bool { return kind == KindChannel || kind == KindVoice }

// StreamPacket is part of a shared screen: one VP8
// picture, or 20 ms of the computer's sound as Opus. Sealed with the voice
// channel's key, like voice, so the guild can't see or hear it.
type StreamPacket struct {
	Channel string `json:"channel"`
	From    string `json:"from,omitempty"` // It is filled in by the guild.
	Kind    string `json:"kind"`           // It is StreamVideo or StreamAudio.
	Key     bool   `json:"key,omitempty"`  // It marks a full picture, which a new viewer can start from.
	Seq     uint32 `json:"seq"`
	Version int    `json:"version"`
	Nonce   []byte `json:"nonce"`
	Cipher  []byte `json:"cipher"`
}

const (
	StreamVideo   = "video"
	StreamAudio   = "audio"
	StreamPreview = "preview" // It is a small JPEG every few seconds, for everyone in the channel.
	StreamCamera  = "camera"  // It is a VP8 camera picture, for everyone in the channel.
)

// Sound is one of a guild's soundboard sounds: up to 10 seconds,
// kept as 20 ms Opus packets (see PackSound).
type Sound struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"` // It is its button's picture ("image_get"), if it has one.
	Hash    string `json:"hash"`              // It is the hash of the packed packets.
	Millis  int    `json:"millis"`
}
