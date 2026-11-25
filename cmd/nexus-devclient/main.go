// nexus-devclient is a small test client for checking a guild from a
// terminal (no window needed, so it also runs on a server). It has a user
// identity (a signing key pair) and can pair with a broadcasting
// guild.
//
//	nexus-devclient id                 show this test user's ID
//	nexus-devclient info ADDRESS:PORT  ask a guild about itself
//	nexus-devclient pair ADDRESS:PORT  pair with a broadcasting guild
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexus/internal/console"
	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const usage = `nexus-devclient - test client for GuildForge guilds

  nexus-devclient id                 show this test user's ID (public key)
  nexus-devclient info ADDRESS:PORT  ask a guild about itself
  nexus-devclient pair ADDRESS:PORT  pair with a broadcasting guild (asks for the OTP)

ADDRESS:PORT is like 192.168.1.20:50000
`

type identity struct {
	Seed []byte `json:"signing_key_seed"`
}

type knownGuilds map[string]string // It maps each address to the guild ID seen there.

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, console.ErrNoInput) {
			fmt.Fprintln(os.Stderr, "\nCancelled.")
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	dir, err := dataDir()
	if err != nil {
		return err
	}
	key, err := loadIdentity(dir)
	if err != nil {
		return err
	}
	userID := guildstore.KeyText(key.Public().(ed25519.PublicKey))

	switch strings.ToLower(args[0]) {
	case "id":
		fmt.Println("Your test user ID:", userID)
		fmt.Println("(stored in", filepath.Join(dir, "identity.json")+")")
		return nil
	case "info", "pair":
		if len(args) != 2 {
			return fmt.Errorf("type the guild's address and port, e.g. nexus-devclient %s 192.168.1.20:50000", args[0])
		}
		addr := args[1]
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return fmt.Errorf("%q needs a port, e.g. %s:50000", addr, addr)
		}
		c, err := connect(dir, addr)
		if err != nil {
			return err
		}
		defer c.Close()
		if strings.ToLower(args[0]) == "info" {
			return info(c)
		}
		return pair(c, key, userID)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func dataDir() (string, error) {
	if d := os.Getenv("NEXUS_DEVCLIENT_HOME"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "nexus-devclient"), nil
}

func loadIdentity(dir string) (ed25519.PrivateKey, error) {
	path := filepath.Join(dir, "identity.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var id identity
		if err := json.Unmarshal(data, &id); err != nil || len(id.Seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("%s is damaged", path)
		}
		return ed25519.NewKeyFromSeed(id.Seed), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	data, _ = json.MarshalIndent(identity{Seed: seed}, "", "  ")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	fmt.Println("Created a new test user identity.")
	return ed25519.NewKeyFromSeed(seed), nil
}

// connect does the encrypted handshake and checks the guild's key against
// the one remembered from last time (trust on first use).
func connect(dir, addr string) (*proto.Conn, error) {
	c, err := proto.Dial(addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("could not connect to %s: %w", addr, err)
	}
	guildID := guildstore.KeyText(c.GuildKey)
	known := knownGuilds{}
	path := filepath.Join(dir, "known-guilds.json")
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &known)
	}
	switch prev, ok := known[addr]; {
	case !ok:
		fmt.Println("First time connecting to", addr)
		fmt.Println("Guild ID:", guildID)
		known[addr] = guildID
		data, _ := json.MarshalIndent(known, "", "  ")
		os.WriteFile(path, data, 0o600)
	case prev != guildID:
		fmt.Println("WARNING: the guild at", addr, "has a different ID than last time!")
		fmt.Println("  before:", prev)
		fmt.Println("  now:   ", guildID)
		fmt.Println("This happens if the guild was replaced - or if someone is pretending to be it.")
	default:
		fmt.Println("Connected to", addr, "(guild ID matches last time)")
	}
	return c, nil
}

func info(c *proto.Conn) error {
	if err := c.Send(proto.Request{Type: "info"}); err != nil {
		return err
	}
	var r proto.Response
	if err := c.Recv(&r); err != nil {
		return err
	}
	fmt.Println("Has an owner: ", yesNo(r.HasOwner))
	fmt.Println("Broadcasting: ", yesNo(r.Broadcasting))
	return nil
}

func pair(c *proto.Conn, key ed25519.PrivateKey, userID string) error {
	con := console.New()
	sig := ed25519.Sign(key, proto.PairProof(c.SessionID))
	for {
		otp, err := con.Ask("One-time password from the host (GuildForge, Pairing): ")
		if err != nil {
			return err
		}
		if err := c.Send(proto.Request{Type: "pair", OTP: otp, UserKey: key.Public().(ed25519.PublicKey), Sig: sig}); err != nil {
			return err
		}
		var r proto.Response
		if err := c.Recv(&r); err != nil {
			return err
		}
		if r.OK {
			fmt.Println("Paired! You are now the owner (ADMIN) of this guild.")
			fmt.Println("Your user ID:", userID)
			return nil
		}
		if r.TriesLeft > 0 {
			word := "tries"
			if r.TriesLeft == 1 {
				word = "try"
			}
			fmt.Printf("%s (%d %s left)\n", r.Error, r.TriesLeft, word)
			continue
		}
		return errors.New(r.Error)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
