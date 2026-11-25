package ui

import (
	"image/color"
	"strconv"
	"time"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// This file shows permissions in the window. The guild decides; the window only
// shows what you may do. The owner may do everything.

// permNames are the permissions as shown (one or two words).
var permNames = map[string]string{
	proto.PermAdministrator: "Administrator", proto.PermManageGuild: "Manage Guild",
	proto.PermManageRoles: "Manage Roles", proto.PermManageChannels: "Manage Channels",
	proto.PermManageJoining: "Manage Joining", proto.PermApproveJoins: "Approve Joins",
	proto.PermKick: "Kick", proto.PermBan: "Ban", proto.PermTimeout: "Timeout",
	proto.PermViewChannel: "View Channel", proto.PermSendMessages: "Send Messages",
	proto.PermAddReactions: "Add Reactions", proto.PermManageMessages: "Manage Messages",
	proto.PermConnect: "Connect", proto.PermSpeak: "Speak", proto.PermStream: "Stream",
	proto.PermPrioritySpeaker: "Priority Speaker", proto.PermMuteMembers: "Mute Members",
	proto.PermDeafenMembers: "Deafen Members", proto.PermMoveMembers: "Move Members",
	proto.PermDisconnectMembers: "Disconnect Members", proto.PermLockChannel: "Lock Channel",
	proto.PermUseSoundboard: "Use Soundboard", proto.PermManageSoundboard: "Manage Soundboard",
	proto.PermPostThreads: "Post Threads", proto.PermEditWiki: "Edit Wiki",
	proto.PermManageEvents: "Manage Events", proto.PermManagePages: "Manage Pages",
	proto.PermChangeNickname: "Change Nickname", proto.PermManageNicknames: "Manage Nicknames",
	proto.PermManageCurrency: "Manage Currency",
}

// permHints explain each permission in plain words.
var permHints = map[string]string{
	proto.PermAdministrator:     "Every permission. Ranks still apply.",
	proto.PermManageGuild:       "Change the name, picture and About text.",
	proto.PermManageRoles:       "Make and change roles below your own, and give them to people.",
	proto.PermManageChannels:    "Make, change and delete channels, folders and categories.",
	proto.PermManageJoining:     "Change Joining Conditions, Rules and the CAPTCHA.",
	proto.PermApproveJoins:      "Approve or decline join requests.",
	proto.PermKick:              "Remove people ranked below you.",
	proto.PermBan:               "Ban people ranked below you, and lift bans.",
	proto.PermTimeout:           "Silence people ranked below you for a while, without removing them.",
	proto.PermViewChannel:       "See the channel and read its messages.",
	proto.PermSendMessages:      "Write messages.",
	proto.PermAddReactions:      "Add emoji reactions.",
	proto.PermManageMessages:    "Delete other people's messages.",
	proto.PermConnect:           "Join voice channels.",
	proto.PermSpeak:             "Talk in voice channels.",
	proto.PermStream:            "Share a screen.",
	proto.PermPrioritySpeaker:   "While your Priority Speaker key is held, others are quieter.",
	proto.PermMuteMembers:       "Silence people ranked below you in voice until you lift it.",
	proto.PermDeafenMembers:     "Stop people ranked below you hearing voice until you lift it.",
	proto.PermMoveMembers:       "Move people ranked below you to another voice channel.",
	proto.PermDisconnectMembers: "Take people ranked below you out of voice.",
	proto.PermLockChannel:       "Lock a voice channel so nobody new can join.",
	proto.PermUseSoundboard:     "Play soundboard sounds in voice.",
	proto.PermManageSoundboard:  "Add and remove soundboard sounds.",
	proto.PermPostThreads:       "Write posts and comments in Threads.",
	proto.PermEditWiki:          "Create, change and delete Wiki pages.",
	proto.PermManageEvents:      "Create, change and delete Calendar events.",
	proto.PermManagePages:       "Turn Calendar, Threads and Wiki on or off, and delete anyone's posts and comments.",
	proto.PermChangeNickname:    "Change your own name in this guild.",
	proto.PermManageNicknames:   "Change the names of people ranked below you.",
	proto.PermManageCurrency:    "Change the currency, make coins, and run the Bank and Shop.",
}

// permGroups order the permissions in editors.
var permGroups = []struct {
	title string
	perms []string
}{
	{"GENERAL", []string{proto.PermAdministrator, proto.PermManageGuild, proto.PermManageRoles, proto.PermManageChannels,
		proto.PermManageJoining, proto.PermApproveJoins, proto.PermKick, proto.PermBan, proto.PermTimeout,
		proto.PermChangeNickname, proto.PermManageNicknames, proto.PermManageCurrency}},
	{"PAGES", []string{proto.PermPostThreads, proto.PermEditWiki, proto.PermManageEvents, proto.PermManagePages}},
	{"CHAT", []string{proto.PermViewChannel, proto.PermSendMessages, proto.PermAddReactions, proto.PermManageMessages}},
	{"VOICE", []string{proto.PermConnect, proto.PermSpeak, proto.PermStream, proto.PermPrioritySpeaker,
		proto.PermMuteMembers, proto.PermDeafenMembers, proto.PermMoveMembers, proto.PermDisconnectMembers, proto.PermLockChannel,
		proto.PermUseSoundboard, proto.PermManageSoundboard}},
}

func has(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}

// can reports whether I may do perm in this guild.
func can(g guildconn.GuildView, perm string) bool {
	if g.IsOwner {
		return true
	}
	return has(g.MyPerms[""], perm)
}

// canIn reports whether I may do perm in this channel.
func canIn(g guildconn.GuildView, channel, perm string) bool {
	if g.IsOwner {
		return true
	}
	if p, ok := g.MyPerms[channel]; ok {
		return has(p, perm)
	}
	return has(g.MyPerms[""], perm)
}

// myRank is my highest role's rank (the owner is above all).
func myRank(g guildconn.GuildView) int {
	if g.IsOwner {
		return 1 << 30
	}
	for _, m := range g.Members {
		if m.IsMe {
			return rankOf(g, m.Roles)
		}
	}
	return 0
}

func rankOf(g guildconn.GuildView, roles []string) int {
	top := 0
	for _, r := range g.Roles {
		if has(roles, r.ID) && r.Rank > top {
			top = r.Rank
		}
	}
	return top
}

// memberOf finds a member's view.
func memberOf(g guildconn.GuildView, key string) *guildconn.MemberView {
	for i := range g.Members {
		if g.Members[i].UserKey == key {
			return &g.Members[i]
		}
	}
	return nil
}

// nameColour is the colour of a member's highest coloured role.
func nameColour(g guildconn.GuildView, key string) color.Color {
	m := memberOf(g, key)
	if m == nil {
		return colText
	}
	for _, r := range g.Roles { // The roles are highest rank first.
		if r.Color != "" && has(m.Roles, r.ID) {
			if c, ok := parseHex(r.Color); ok {
				return c
			}
		}
	}
	return colText
}

func parseHex(s string) (color.NRGBA, bool) {
	if len(s) != 7 || s[0] != '#' {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
}

// timeoutEnds is when a member's timeout ends ("" when there is none),
// as a time of day, or a date when it isn't today.
func timeoutEnds(m guildconn.MemberView) string {
	t, err := time.Parse(time.RFC3339, m.TimeoutUntil)
	if err != nil || !time.Now().Before(t) {
		return ""
	}
	t = t.Local()
	if y, mo, d := t.Date(); y == time.Now().Year() && mo == time.Now().Month() && d == time.Now().Day() {
		return t.Format("15:04")
	}
	return t.Format("15:04 on 2 January")
}

// myTimeout is when your own timeout in a guild ends, or "".
func myTimeout(g guildconn.GuildView) string {
	for _, m := range g.Members {
		if m.IsMe {
			return timeoutEnds(m)
		}
	}
	return ""
}

// myWaitEnds is when your Waiting Period in a guild ends, as a time of
// day (or a date), or "" when you may post. The guild decides; this
// only explains why the message box is grey.
func myWaitEnds(g guildconn.GuildView) string {
	if g.Join == nil || g.Join.PostWaitMinutes <= 0 || g.IsOwner {
		return ""
	}
	for _, m := range g.Members {
		if !m.IsMe {
			continue
		}
		for _, r := range m.Roles {
			if r != proto.EveryoneRole {
				return "" // Members with a role don't wait.
			}
		}
		joined, err := time.Parse(time.RFC3339, m.Joined)
		if err != nil {
			return ""
		}
		return timeoutEnds(guildconn.MemberView{TimeoutUntil: joined.Add(time.Duration(g.Join.PostWaitMinutes) * time.Minute).Format(time.RFC3339)})
	}
	return ""
}
