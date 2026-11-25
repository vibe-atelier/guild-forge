package ui

import (
	_ "embed" // This import is needed for go:embed.

	"fyne.io/fyne/v2"
)

// These are the app's own icons, drawn as SVG (the same shapes as the HTML app's).

// logoSVG is the Nexus logo. It is a file of its own so the programs' icon
// (tools/make-icon) is drawn from the same picture.
//
//go:embed logo.svg
var logoSVG string

var logoIcon = svg("logo.svg", logoSVG)

var folderIcon = svg("folder.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#ff9ca3" stroke-width="1.8" stroke-linejoin="round" d="M3 7.5A1.5 1.5 0 0 1 4.5 6h4.4l2 2.2h8.6A1.5 1.5 0 0 1 21 9.7v8.8a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18.5z"/>
<circle cx="12" cy="14" r="2.2" fill="none" stroke="#ff9ca3" stroke-width="1.8"/>
<path stroke="#ff9ca3" stroke-width="1.8" stroke-linecap="round" d="M14 14h4m-1.5 0v2"/>
</svg>`)

var userPlusIcon = svg("user-plus.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#ff9ca3" stroke-width="1.8" stroke-linecap="round"><circle cx="10" cy="8" r="4"/><path d="M3 20c.8-3.6 3.6-5.5 7-5.5s6.2 1.9 7 5.5M19 7v6m-3-3h6"/></g>
</svg>`)

var backIcon = svg("back.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#f4f4f6" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M15 5l-7 7 7 7"/>
</svg>`)

var xIcon = svg("x.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#8b8b96" stroke-width="2" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
</svg>`)

var lockIcon = svg("lock.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#f4f4f6" stroke-width="1.8" stroke-linecap="round"><rect x="5" y="10.5" width="14" height="10" rx="2.5"/><path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/></g>
</svg>`)

func svg(name, src string) fyne.Resource { return fyne.NewStaticResource(name, []byte(src)) }

var copyIcon = svg("copy.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8"><rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6.5A2.5 2.5 0 0 0 13.5 4h-7A2.5 2.5 0 0 0 4 6.5v7A2.5 2.5 0 0 0 6.5 16H8"/></g>
</svg>`)

var dotsIcon = svg("dots.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="#8b8b96"><circle cx="5" cy="12" r="1.8"/><circle cx="12" cy="12" r="1.8"/><circle cx="19" cy="12" r="1.8"/></g>
</svg>`)

var infoIcon = svg("info.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><circle cx="12" cy="8" r=".6" fill="#8b8b96"/></g>
</svg>`)

var shieldIcon = svg("shield.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linejoin="round" d="M12 3l7.5 3v5.5c0 4.6-3.2 8.2-7.5 9.5-4.3-1.3-7.5-4.9-7.5-9.5V6z"/>
</svg>`)

var usersIcon = svg("users.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linecap="round"><circle cx="9" cy="8" r="3.5"/><path d="M2.5 19.5c.7-3.3 3.3-5 6.5-5s5.8 1.7 6.5 5M16 4.8a3.5 3.5 0 0 1 0 6.4M18.5 14.8c1.6.8 2.7 2.4 3 4.7"/></g>
</svg>`)

var checkIcon = svg("check.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#22c55e" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" d="M5 12.5l4.5 4.5L19 7.5"/>
</svg>`)

var crossIcon = svg("cross.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#ff5a36" stroke-width="2.4" stroke-linecap="round" d="M6 6l12 12M18 6L6 18"/>
</svg>`)

var gearIcon = svg("gear.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#5a5a64" stroke-width="1.8"><circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.4M12 18.8v2.4M2.8 12h2.4M18.8 12h2.4M5.5 5.5l1.7 1.7M16.8 16.8l1.7 1.7M5.5 18.5l1.7-1.7M16.8 7.2l1.7-1.7"/></g>
</svg>`)

var replyIcon = svg("reply.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#8b8b96" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" d="M10 8L5 12l5 4M5 12h9a5 5 0 0 1 5 5v1"/>
</svg>`)

var calendarIcon = svg("calendar.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linecap="round"><rect x="3.5" y="5" width="17" height="15.5" rx="2.5"/><path d="M3.5 10h17M8 3v4M16 3v4"/></g>
</svg>`)

var threadsIcon = svg("threads.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linejoin="round"><path d="M4 5h16v10H9l-5 4z"/><path d="M8 9h8M8 12h5" stroke-linecap="round"/></g>
</svg>`)

var wikiIcon = svg("wiki.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linejoin="round"><path d="M12 6.5C10 5 7 4.5 3.5 5v13.5c3.5-.5 6.5 0 8.5 1.5 2-1.5 5-2 8.5-1.5V5C17 4.5 14 5 12 6.5z"/><path d="M12 6.5v13.5"/></g>
</svg>`)

// chatIcon is a speech bubble (for sending a private message).
var chatIcon = svg("chat.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#c9c9d1" stroke-width="1.8" stroke-linejoin="round" d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v8a2.5 2.5 0 0 1-2.5 2.5H10l-4.5 3.5V17h0A1.5 1.5 0 0 1 4 15.5z"/>
</svg>`)

// smileIcon is the emoji picker's button.
var smileIcon = svg("smile.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#c9c9d1" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="8.5"/><path d="M8.5 14.2c.9 1.3 2.1 2 3.5 2s2.6-.7 3.5-2"/></g>
<g fill="#c9c9d1"><circle cx="9.2" cy="10" r="1.2"/><circle cx="14.8" cy="10" r="1.2"/></g>
</svg>`)

// clipIcon is the button for attaching a file.
var clipIcon = svg("clip.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#c9c9d1" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M19 11.5l-6.6 6.6a4.2 4.2 0 0 1-6-6l7-7a2.8 2.8 0 0 1 4 4l-7 7a1.4 1.4 0 0 1-2-2l6.4-6.4"/>
</svg>`)

// dirIcon is a plain folder, for folders in the channel list.
var dirIcon = svg("dir.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="#8b8b96" fill-opacity=".25" stroke="#a3a3ad" stroke-width="1.7" stroke-linejoin="round" d="M3 7.5A1.5 1.5 0 0 1 4.5 6h4.4l2 2.2h8.6A1.5 1.5 0 0 1 21 9.7v8.8a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18.5z"/>
</svg>`)

// plusIcon is the rail's Add Guild button, drawn so it sits exactly centred.
var plusIcon = svg("plus.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<path fill="none" stroke="#22c55e" stroke-width="2.4" stroke-linecap="round" d="M12 5v14M5 12h14"/>
</svg>`)

// cogIcon is a clear settings cog (GuildForge's Manage Guild).
var cogIcon = svg("cog.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#e5e5ea" stroke-width="1.7" stroke-linejoin="round"><path d="M10.3 2.5h3.4l.5 2.6 1.6.7 2.2-1.5 2.4 2.4-1.5 2.2.7 1.6 2.6.5v3.4l-2.6.5-.7 1.6 1.5 2.2-2.4 2.4-2.2-1.5-1.6.7-.5 2.6h-3.4l-.5-2.6-1.6-.7-2.2 1.5-2.4-2.4 1.5-2.2-.7-1.6-2.6-.5v-3.4l2.6-.5.7-1.6-1.5-2.2 2.4-2.4 2.2 1.5 1.6-.7z"/><circle cx="12" cy="12" r="3.2"/></g></svg>`)

var searchIcon = svg("search.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.9" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6"/><path d="M15 15l5 5"/></g>
</svg>`)
