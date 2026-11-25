// Package console reads answers typed by the user in the terminal: normal
// lines, yes/no questions, and passwords that are hidden while typed.
package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

// ErrNoInput means the input ended (for example Ctrl+D, or a closed pipe)
// before the user gave an answer.
var ErrNoInput = errors.New("no input given")

// Console asks questions and reads the answers. All questions share one
// reader, so nothing the user typed is lost between questions.
type Console struct {
	in       *bufio.Reader
	out      io.Writer
	terminal bool // It is true when reading from the real keyboard (passwords are hidden then).
}

// New returns a Console that reads from the keyboard and writes to the screen.
func New() *Console {
	return &Console{in: bufio.NewReader(os.Stdin), out: os.Stdout, terminal: true}
}

// NewWith returns a Console that reads from in and writes to out. Passwords
// are not hidden. Used by tests.
func NewWith(in io.Reader, out io.Writer) *Console {
	return &Console{in: bufio.NewReader(in), out: out}
}

// Out is where the Console writes its messages.
func (c *Console) Out() io.Writer { return c.out }

// Ask shows prompt and returns the line the user types (without the Enter).
func (c *Console) Ask(prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	return c.readLine()
}

func (c *Console) readLine() (string, error) {
	line, err := c.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if line == "" {
				return "", ErrNoInput
			}
			// Last line without an Enter at the end: still use it.
		} else {
			return "", err
		}
	}
	// Some tools (e.g. Windows PowerShell) put an invisible "byte order mark"
	// at the start of piped text; it is never part of a real answer.
	line = strings.TrimPrefix(line, string(rune(0xFEFF))) // This removes a byte order mark.
	return strings.TrimRight(line, "\r\n"), nil
}

// AskPassword shows prompt and reads a line without showing what is typed.
// If the input is not a real keyboard (e.g. piped in), it reads normally.
func (c *Console) AskPassword(prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	var line string
	var readErr error
	ok := c.hidden(func() { line, readErr = c.readLine() })
	if !ok {
		// Not a terminal (e.g. piped in): nothing to hide from.
		return c.readLine()
	}
	fmt.Fprintln(c.out) // The Enter key was hidden too, so move to a new line.
	return line, readErr
}

// hidden runs fn while typing is hidden. It returns false without running fn
// when the input is not a terminal.
func (c *Console) hidden(fn func()) bool {
	if !c.terminal {
		return false
	}
	restore, err := hideTyping()
	if err != nil {
		return false
	}

	// If the user presses Ctrl+C meanwhile, put the terminal back before
	// quitting, otherwise it would stay "blind".
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			restore()
			fmt.Fprintln(c.out)
			os.Exit(130)
		case <-done:
		}
	}()

	fn()
	close(done)
	signal.Stop(sig)
	restore()
	return true
}

// Confirm asks a yes/no question. Pressing Enter alone gives def.
func (c *Console) Confirm(prompt string, def bool) (bool, error) {
	hint := " [y/N] "
	if def {
		hint = " [Y/n] "
	}
	for {
		ans, err := c.Ask(prompt + hint)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(c.out, "Please answer y or n.")
	}
}

// SetOut sends the Console's messages to w instead.
func (c *Console) SetOut(w io.Writer) { c.out = w }
