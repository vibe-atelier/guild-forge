//go:build notices

package main

import _ "embed" // This import is needed for go:embed.

// notices is the app's third-party notices, shown on its Licenses pages. The
// build script writes NOTICES.txt with tools/notices just before building.
//
//go:embed NOTICES.txt
var notices string

// hasNotices is true when the real notices are built in.
const hasNotices = true
