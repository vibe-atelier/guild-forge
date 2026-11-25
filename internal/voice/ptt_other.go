//go:build !windows

package voice

// Elsewhere, Push-To-Talk works while Nexus is the window in front (the
// window reports the key with SetKeyHeld).
func globalKeyHeld(string) bool { return false }
