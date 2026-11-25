// Package single keeps one copy of the app running per settings folder. The
// first copy listens on this computer only (127.0.0.1, a random port) and
// writes the port and a random password to a file only this user can read; a
// second copy finds it, asks the first to come to the front, and closes.
package single

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the file in the settings folder that says where this
// account's running copy listens. The settings folder is shared by every
// account on the PC, so each account has its own file: two people
// signed in at once each run their own copy.
func FileName() string {
	name := "running"
	if u, err := user.Current(); err == nil {
		name += "-" + strings.Map(func(r rune) rune {
			if r == '\\' || r == '/' || r == ':' || r == ' ' {
				return '_'
			}
			return r
		}, u.Username)
	}
	return name + ".txt"
}

// Claim reports whether another copy is already running (and has been asked
// to show itself). Otherwise this copy becomes the one: wake receives each
// time a later copy asks it to come to the front, until stop is called.
func Claim(dir string) (already bool, wake <-chan struct{}, stop func()) {
	path := filepath.Join(dir, FileName())
	if askOther(path) {
		return true, nil, func() {}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, nil, func() {} // There is no guard, but the app still works.
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	word := hex.EncodeToString(secret)
	os.WriteFile(path, []byte(l.Addr().String()+" "+word+"\n"), 0o600)
	ch := make(chan struct{}, 1)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(2 * time.Second))
				line, _ := bufio.NewReader(c).ReadString('\n')
				if strings.TrimSpace(line) != "show "+word {
					return
				}
				c.Write([]byte("ok\n"))
				select {
				case ch <- struct{}{}:
				default:
				}
			}()
		}
	}()
	return false, ch, func() {
		l.Close()
		os.Remove(path)
	}
}

// askOther asks a running copy to show itself; false if there is none.
func askOther(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	addr, word, ok := strings.Cut(strings.TrimSpace(string(data)), " ")
	if !ok || !strings.HasPrefix(addr, "127.0.0.1:") {
		return false
	}
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false // The file is left over from a copy that stopped.
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte("show " + word + "\n")); err != nil {
		return false
	}
	line, _ := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line) == "ok"
}
