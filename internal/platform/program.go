package platform

import "os"

// programPath is this program's own file, read once at start-up, before an
// update can rename it.
var programPath, programErr = os.Executable()

// ProgramPath is the file of the running program. Guilds are started with
// it, and the sign-in task runs it, so GuildForge.exe and guild-forge each
// work on their own.
func ProgramPath() (string, error) { return programPath, programErr }
