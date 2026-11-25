package ui

import (
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
)

// This file handles joining a guild: check the address, read the About page and
// Rules, meet the join rules, answer the CAPTCHA.

var joinRefusals = map[string]string{
	"banned":      "You are banned from this guild.",
	"not_invited": "This guild is invite-only, and your user ID is not on its list. Send your user ID to the owner.",
}

func (u *UI) joinPane(done func()) fyne.CanvasObject {
	checkBtn := primary("Check", nil)
	addr := newAddressBox()
	problem := newErrorText()

	check := func() {
		u.checkGuildAt(checkBtn, addr.Text(), problem, func(c *guildconn.PairCheck) {
			if c.Known {
				problem.Set(errors.New("This guild is already in your list."))
				return
			}
			done()
			u.startJoin(c) // The guild's About page opens as a page.
		})
	}
	checkBtn.OnTapped = check
	addr.link.OnSubmitted = func(string) { check() }
	addr.port.OnSubmitted = func(string) { check() }
	u.focusLater(addr.link)
	if a := u.devAddress; a != "" { // This is only for the guide's pictures.
		u.devAddress = ""
		addr.Set(a)
		check()
	}

	return form(
		para("Type the address and port that the guild's owner gave you."),
		addr.box,
		problem.box,
		buttonsRow(widget.NewButton("Cancel", done), checkBtn),
	)
}

// checkGuildAt asks the guild at address who it is (Add Guild's Join and
// Pair Owner tabs). btn says "Checking…" meanwhile; a problem shows in
// problem, otherwise found gets the answer.
func (u *UI) checkGuildAt(btn *widget.Button, address string, problem *errorText, found func(*guildconn.PairCheck)) {
	problem.Set(nil)
	btn.Disable()
	btn.SetText("Checking…")
	go func() {
		c, err := u.guilds.Check(address)
		fyne.Do(func() {
			btn.Enable()
			btn.SetText("Check")
			if err != nil {
				problem.Set(err)
				return
			}
			found(c)
		})
	}()
}
