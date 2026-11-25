package ui

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
)

// This file is Add Guild: Join (step 4) or Pair Owner.

func (u *UI) openAddGuild() { u.openAddGuildOn("join") }

func (u *UI) openAddGuildOn(tab string) {
	var p *popup
	pane := container.NewStack()
	show := func(i int) {
		if i == 0 {
			pane.Objects = []fyne.CanvasObject{u.joinPane(func() { p.Close() })}
		} else {
			pane.Objects = []fyne.CanvasObject{u.pairPane(func() { p.Close() })}
		}
		pane.Refresh()
	}
	first := 0
	if tab == "pair" {
		first = 1
	}
	closeBtn := widget.NewButtonWithIcon("", xIcon, func() { p.Close() })
	closeBtn.Importance = widget.LowImportance
	head := container.NewBorder(nil, nil, nil, closeBtn, text("Add Guild", 22, colText, true))
	p = u.openPopup(560, head, tabBar(nil, []string{"Join", "Pair Owner"}, first, show), pane)
	show(first)
}

// addressBox holds the Link (IP Address) and Port boxes. A port typed
// after a colon in the first box wins, and the Port box is then ignored.
type addressBox struct {
	link, port *widget.Entry
	box        fyne.CanvasObject
}

func newAddressBox() *addressBox {
	a := &addressBox{link: widget.NewEntry(), port: widget.NewEntry()}
	a.link.SetPlaceHolder("127.0.0.1")
	a.port.SetPlaceHolder("50000")
	a.box = container.NewBorder(nil, nil, nil, container.New(fixedWidth{120}, field("Port", a.port, "")),
		field("Link (IP Address)", a.link, ""))
	return a
}

// Text is the address to use.
func (a *addressBox) Text() string { return joinAddress(a.link.Text, a.port.Text) }

// Set shows an address, split back into its two boxes.
func (a *addressBox) Set(addr string) {
	if host, port, err := net.SplitHostPort(addr); err == nil {
		a.link.SetText(host)
		a.port.SetText(port)
		return
	}
	a.link.SetText(addr)
}

func joinAddress(link, port string) string {
	link, port = strings.TrimSpace(link), strings.TrimSpace(port)
	for _, p := range []string{"nexus://", "http://", "https://"} {
		link = strings.TrimPrefix(link, p)
	}
	link = strings.TrimSuffix(link, "/")
	// An IPv6 address is full of colons: those are not a port.
	if bare := strings.TrimSuffix(strings.TrimPrefix(link, "["), "]"); net.ParseIP(bare) != nil && strings.Contains(bare, ":") {
		if port == "" {
			return "[" + bare + "]"
		}
		return net.JoinHostPort(bare, port)
	}
	if link == "" || port == "" || strings.Contains(link, ":") {
		return link
	}
	return net.JoinHostPort(link, port)
}

// pairPane pairs as a new guild's owner: check the address, then type the
// one-time password that GuildForge shows.
func (u *UI) pairPane(done func()) fyne.CanvasObject {
	checkBtn := widget.NewButton("Check", nil)
	addr := newAddressBox()
	otp := widget.NewEntry()
	otp.SetPlaceHolder(exampleOTP()) // It shows a new made-up example each time.
	problem := newErrorText()
	found := stack(4) // This is a column, so text lines up.
	found.Hide()
	otpField := field("One-Time Password", otp, "GuildForge shows it on the guild's computer, under Pairing.")
	otpField.Hide()

	var checked *guildconn.PairCheck
	var pairBtn *widget.Button
	pairBtn = primary("Pair", nil)
	pairBtn.Disable()

	check := func() {
		problem.Set(nil)
		checked = nil
		pairBtn.Disable()
		otpField.Hide()
		found.RemoveAll()
		found.Hide()
		u.checkGuildAt(checkBtn, addr.Text(), problem, func(c *guildconn.PairCheck) {
			addr.Set(c.Address) // Set the address before remembering the check, because changing the boxes forgets it.
			checked = c
			state, ok := "Broadcasting…", true
			colour := colOnline
			switch {
			case c.Known:
				state, ok = "You are already in this guild.", false
			case c.HasOwner:
				state, ok = "This guild already has an owner.", false
			case !c.Broadcasting:
				state, ok = "Not broadcasting. Start it in GuildForge, under Pairing.", false
			}
			if !ok {
				colour = colDanger
			}
			info := container.NewVBox(text(c.Name, 17, colText, true),
				text("Guild ID "+shortID(c.GuildID), 12.5, colFaint, false),
				wrapText(state, colour))
			found.Add(card(14, container.NewBorder(nil, nil, container.NewHBox(avatarSquare(c.Name, c.GuildID, 48), gap(1)), nil, info)))
			found.Show()
			found.Refresh()
			if ok {
				otpField.Show()
				pairBtn.Enable()
				u.win.Canvas().Focus(otp)
			}
		})
	}
	checkBtn.OnTapped = check
	addr.link.OnSubmitted = func(string) { check() }
	addr.port.OnSubmitted = func(string) { check() }
	addr.link.OnChanged = func(string) { checked = nil; pairBtn.Disable() }
	addr.port.OnChanged = addr.link.OnChanged
	if a := u.devAddress; a != "" { // This is only for the guide's pictures.
		u.devAddress = ""
		addr.Set(a)
		check()
	}

	pair := func() {
		if checked == nil {
			return
		}
		problem.Set(nil)
		c, code := checked, otp.Text
		pairBtn.Disable()
		pairBtn.SetText("Pairing…")
		go func() {
			r, err := u.guilds.Pair(c.Address, c.GuildID, code)
			fyne.Do(func() {
				pairBtn.SetText("Pair")
				pairBtn.Enable()
				switch {
				case err != nil:
					problem.Set(err)
				case !r.OK:
					msg := r.Error
					if r.TriesLeft > 0 {
						msg += fmt.Sprintf(" (%d %s left)", r.TriesLeft, map[bool]string{true: "try", false: "tries"}[r.TriesLeft == 1])
					} else {
						pairBtn.Disable()
					}
					problem.Set(errors.New(msg))
				default:
					done()
					u.toast("Paired! You are now the owner (ADMIN) of this guild.")
					u.selectGuild(c.GuildID)
					// Give the new guild a name straight away.
					go func() {
						for i := 0; i < 20; i++ {
							if g := u.findGuild(c.GuildID); g != nil && g.Status == "online" {
								fyne.Do(func() { u.openGuildSettings(*g, "overview") })
								return
							}
							waitShort()
						}
					}()
				}
			})
		}()
	}
	pairBtn.OnTapped = pair
	otp.OnSubmitted = func(string) { pair() }

	hint := para("On the guild's computer, open GuildForge, pick the guild in Manage Guild, and start broadcasting under Pairing.")
	return form(
		hint,
		addr.box,
		found,
		otpField,
		problem.box,
		buttonsRow(widget.NewButton("Cancel", done), checkBtn, pairBtn),
	)
}

// exampleOTP is a made-up one-time password in GuildForge's form
// (XXX-XXX-XXX), different each time, so nobody mistakes it for a real one.
func exampleOTP() string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 0, 11)
	for i := 0; i < 9; i++ {
		if i == 3 || i == 6 {
			b = append(b, '-')
		}
		b = append(b, letters[rand.IntN(len(letters))])
	}
	return string(b)
}
