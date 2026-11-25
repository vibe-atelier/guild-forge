package proto

import (
	"crypto/ed25519"
	"errors"
	"sync"
	"time"
)

// Session is the client side of a live connection: it can send
// requests and wait for their answers while events arrive at any time.
type Session struct {
	c *Conn

	mu      sync.Mutex
	nextID  uint64
	waiting map[uint64]chan Response
	err     error

	// Events receives the guild's events. It is closed when the connection ends.
	Events chan Response
	// Voice receives voice packets, apart from the other events so
	// that neither crowds out the other. Old packets are dropped when full.
	Voice chan Response
	done  chan struct{}
}

// ErrClosed means the connection has ended.
var ErrClosed = errors.New("connection closed")

// NewSession starts reading from c in the background.
func NewSession(c *Conn) *Session {
	s := &Session{c: c, waiting: map[uint64]chan Response{}, Events: make(chan Response, 32), Voice: make(chan Response, 128), done: make(chan struct{})}
	go s.readLoop()
	return s
}

func (s *Session) readLoop() {
	defer close(s.Events)
	defer close(s.Voice)
	for {
		s.c.SetDeadline(time.Time{})
		var r Response
		if err := s.c.Recv(&r); err != nil {
			s.mu.Lock()
			s.err = err
			for id, ch := range s.waiting {
				close(ch)
				delete(s.waiting, id)
			}
			s.mu.Unlock()
			close(s.done)
			return
		}
		if r.Type == "voice" || r.Type == "stream" {
			select {
			case s.Voice <- r:
			default: // Too many are waiting, so this one is dropped.
			}
			continue
		}
		if r.Type == "event" || r.ID == 0 {
			select {
			case s.Events <- r:
			default: // Nobody is listening fast enough, so it is dropped.
			}
			continue
		}
		s.mu.Lock()
		ch := s.waiting[r.ID]
		delete(s.waiting, r.ID)
		s.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

// Call sends req and waits up to timeout for the answer.
func (s *Session) Call(req Request, timeout time.Duration) (Response, error) {
	ch := make(chan Response, 1)
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return Response{}, ErrClosed
	}
	s.nextID++
	req.ID = s.nextID
	s.waiting[req.ID] = ch
	s.mu.Unlock()

	if err := s.c.Send(req); err != nil {
		s.mu.Lock()
		delete(s.waiting, req.ID)
		s.mu.Unlock()
		return Response{}, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return Response{}, ErrClosed
		}
		return r, nil
	case <-time.After(timeout):
		s.mu.Lock()
		delete(s.waiting, req.ID)
		s.mu.Unlock()
		return Response{}, errors.New("the guild did not answer in time")
	}
}

// Done is closed when the connection has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close ends the connection.
func (s *Session) Close() error { return s.c.Close() }

// GuildKey is the guild's public key (checked during the handshake).
func (s *Session) GuildKey() ed25519.PublicKey { return s.c.GuildKey }

// SessionID identifies this connection's handshake.
func (s *Session) SessionID() [32]byte { return s.c.SessionID }

// AuthProof is what a user signs to prove who they are on this connection,
// including which computer they use.
func AuthProof(sessionID [32]byte, deviceID string) []byte {
	b := append([]byte("nexus-auth-v1"), sessionID[:]...)
	return append(b, deviceID...)
}

// Post sends req without waiting for an answer (voice packets).
func (s *Session) Post(req Request) error {
	req.ID = 0
	return s.c.Send(req)
}
