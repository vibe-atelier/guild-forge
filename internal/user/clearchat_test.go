package user

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClearChat checks that the chat goes, and its files go only with Erase Media.
func TestClearChat(t *testing.T) {
	u, err := Create(t.TempDir(), "Alice", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	contact := strings.Repeat("A", 52)
	if err := u.SaveChat(contact, []ChatEntry{{ID: "1", From: contact, Text: "hello"}}); err != nil {
		t.Fatal(err)
	}
	rel, err := u.SaveChatFile(contact, "2", "photo.png", []byte("picture"))
	if err != nil {
		t.Fatal(err)
	}
	if err := u.ClearChat(contact, false); err != nil {
		t.Fatal(err)
	}
	if chat, _ := u.LoadChat(contact); len(chat) != 0 {
		t.Fatal("the messages are gone")
	}
	if _, err := os.Stat(filepath.Join(u.Dir, rel)); err != nil {
		t.Fatal("without Erase Media, the files stay")
	}
	if err := u.ClearChat(contact, true); err != nil {
		t.Fatal("clearing an empty chat again works")
	}
	if _, err := os.Stat(filepath.Join(u.Dir, rel)); !os.IsNotExist(err) {
		t.Fatal("with Erase Media, the files go too")
	}
	if err := u.ClearChat("../escape", true); err == nil {
		t.Fatal("a bad contact ID is refused")
	}
}
