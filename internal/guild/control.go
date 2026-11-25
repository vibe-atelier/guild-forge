package guild

// This file is the control channel: how GuildForge commands talk to a running
// guild. The guild listens on a random port on 127.0.0.1 (this computer
// only) and writes that port and a secret token into
// <guild>/runtime/control.json, which only the guild's owner account and
// administrators can read. A command must send the token with each request.

import (
	"nexus/internal/fsutil"
	"nexus/internal/guildstore"
	"nexus/internal/portmap"

	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const ControlFileName = "control.json"

type controlFile struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

// ControlRequest is sent by a GuildForge command.
type ControlRequest struct {
	Token string `json:"token"`
	Cmd   string `json:"cmd"` // It is status, stop, broadcast, end-broadcast, otp or reset-owner.
}

// ControlResponse is the running guild's answer.
type ControlResponse struct {
	OK            bool      `json:"ok"`
	Error         string    `json:"error,omitempty"`
	Port          int       `json:"port,omitempty"` // It is the port the guild is listening on.
	Broadcasting  bool      `json:"broadcasting,omitempty"`
	BroadcastEnds time.Time `json:"broadcast_ends,omitzero"`
	HasOwner      bool      `json:"has_owner,omitempty"`
	OwnerKey      string    `json:"owner_key,omitempty"`
	OTP           string    `json:"otp,omitempty"`
	OTPExpires    time.Time `json:"otp_expires,omitzero"`
	Members       int       `json:"members,omitempty"` // For "status", it is how many members there are.
	Online        int       `json:"online,omitempty"`  // It is how many members are connected.
	// It holds the router and the addresses to give people.
	Public *portmap.Status `json:"public,omitempty"`
}

// StartControl starts the control listener for g and writes control.json.
func StartControl(g *Server, dataDir string, guildPort int) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		l.Close()
		return nil, err
	}
	cf := controlFile{Port: l.Addr().(*net.TCPAddr).Port, Token: hex.EncodeToString(tok), PID: os.Getpid()}
	data, _ := json.Marshal(cf)
	if err := os.MkdirAll(filepath.Join(dataDir, guildstore.RuntimeDir), 0o700); err != nil {
		l.Close()
		return nil, err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dataDir, guildstore.RuntimeDir, ControlFileName), data, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go handleControl(g, conn, cf.Token, guildPort)
		}
	}()
	return l, nil
}

func handleControl(g *Server, conn net.Conn, token string, guildPort int) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	var req ControlRequest
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		return
	}
	reply := func(r ControlResponse) { json.NewEncoder(conn).Encode(r) }
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		reply(ControlResponse{Error: "bad token"})
		return
	}
	switch req.Cmd {
	case "status":
		r := g.Status()
		r.Port = guildPort
		reply(r)
	case "stop":
		reply(ControlResponse{OK: true})
		g.Log.Printf("stop requested")
		g.Stop()
	case "broadcast":
		until, err := g.StartBroadcast()
		if err != nil {
			reply(ControlResponse{Error: err.Error()})
			return
		}
		reply(ControlResponse{OK: true, Broadcasting: true, BroadcastEnds: until})
	case "reset-owner":
		if err := g.ResetOwner(); err != nil {
			reply(ControlResponse{Error: err.Error()})
			return
		}
		reply(ControlResponse{OK: true})
	case "end-broadcast":
		g.EndBroadcast()
		reply(ControlResponse{OK: true})
	case "otp":
		otp, exp, err := g.NewOTP()
		if err != nil {
			reply(ControlResponse{Error: err.Error()})
			return
		}
		reply(ControlResponse{OK: true, OTP: otp, OTPExpires: exp})
	default:
		reply(ControlResponse{Error: "unknown command " + strconv.Quote(req.Cmd)})
	}
}

// ErrNotRunning means the guild is not running.
var ErrNotRunning = errors.New("the guild is not running")

// Call sends cmd to the guild running from dataDir.
func Call(dataDir, cmd string) (*ControlResponse, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, guildstore.RuntimeDir, ControlFileName))
	if err != nil {
		return nil, ErrNotRunning
	}
	var cf controlFile
	if err := json.Unmarshal(data, &cf); err != nil || cf.Port == 0 {
		return nil, ErrNotRunning
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cf.Port)), time.Second)
	if err != nil {
		return nil, ErrNotRunning // The file was left over from a guild that did not shut down cleanly.
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(ControlRequest{Token: cf.Token, Cmd: cmd}); err != nil {
		return nil, err
	}
	var r ControlResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&r); err != nil {
		return nil, errors.New("the guild did not answer")
	}
	if !r.OK {
		return &r, errors.New(r.Error)
	}
	return &r, nil
}

// RunningStatus asks a guild for its status. running is false if it is not running.
func RunningStatus(dataDir string) (s *ControlResponse, running bool) {
	r, err := Call(dataDir, "status")
	if err != nil {
		return nil, false
	}
	return r, true
}
