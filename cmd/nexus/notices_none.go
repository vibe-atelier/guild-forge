//go:build !notices

package main

// notices is empty when the app is built without the build script, which
// writes the real notices; the Licenses page then says so.
const notices = ""

// hasNotices is false when the real notices are not built in.
const hasNotices = false
