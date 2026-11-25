//go:build windows && !notices

package main

// notices stands in when GuildForge is built without the build script,
// which writes the real notices.
const notices = "This copy of GuildForge was built without its notices. Build it with build.ps1 to include them."
