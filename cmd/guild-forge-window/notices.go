//go:build windows && notices

package main

import _ "embed" // This import is needed for go:embed.

// notices is GuildForge's third-party notices, shown on its Licenses page. The
// build script writes NOTICES.txt with tools/notices just before building.
//
//go:embed NOTICES.txt
var notices string
