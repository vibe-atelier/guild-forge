// Package platform holds everything that differs between Windows and Linux:
// where guild-forge keeps its files, the guild-forge system user and file
// ownership on Linux, and starting guilds in the background (systemd on
// Linux, a hidden process and a sign-in task on Windows).
package platform
