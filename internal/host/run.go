package host

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/portmap"
)

// Run is "guild-forge run=<guild ID>": the guild process itself. It is
// started by "start" (or at boot), not typed by people.
func Run(configDir, id string) error {
	reg, err := LoadRegistry(configDir)
	if err != nil {
		return err
	}
	e := reg.GetByID(id)
	if e == nil {
		return fmt.Errorf("no guild with ID %s is on this computer", id)
	}
	if e.Port == 0 {
		return fmt.Errorf("GUILD#%d has no port; give it one with guild-forge setup=%d port=50000", e.Index, e.Index)
	}
	if s, running := guild.RunningStatus(e.DataDir); running && s != nil {
		return fmt.Errorf("GUILD#%d is already running", e.Index)
	}

	logger, closeLog := guild.OpenLog(e.DataDir)
	defer closeLog()

	g, err := guild.NewServer(e.DataDir, logger)
	if err != nil {
		logger.Printf("cannot start: %v", err)
		return err
	}

	host := os.Getenv("GUILD_FORGE_LISTEN") // An empty setting means every network address.
	l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(e.Port)))
	if err != nil {
		logger.Printf("cannot listen on port %d: %v", e.Port, err)
		return err
	}
	defer l.Close()

	// Ask the router to open the port, and find the addresses to
	// give people. Withdrawn when the guild stops or is deleted
	// (deleting stops it first); a power cut leaves it for at most an hour.
	portmap.Mode = os.Getenv("GUILD_FORGE_ROUTER") // It is "demo" for pictures and "off" for tests.
	keep := portmap.Open(e.Port, !e.NoAutoPort, logger)
	defer keep.Close()
	g.SetPublic(keep, e.Port)

	ctl, err := guild.StartControl(g, e.DataDir, e.Port)
	if err != nil {
		logger.Printf("cannot start the control channel: %v", err)
		return err
	}
	defer ctl.Close()
	defer os.Remove(filepath.Join(e.DataDir, guildstore.RuntimeDir, guild.ControlFileName))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case s := <-sig:
			logger.Printf("received %v, stopping", s)
			g.Stop()
		case <-g.Done():
		}
	}()
	go func() {
		<-g.Done()
		l.Close()
		ctl.Close()
	}()

	go runAutoBackups(configDir, id, g, logger) // Automatic backups run while the guild runs.

	logger.Printf("GUILD %s running on port %d", g.ID, e.Port)
	g.Serve(l)
	logger.Printf("stopped")
	return nil
}

// LocalAddresses lists this computer's network addresses (to tell people
// what to type in the client).
func LocalAddresses() []string {
	var list []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		list = append(list, ipn.IP.String())
	}
	return list
}
