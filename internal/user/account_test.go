package user

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAccount checks Change Password, Recovery Code, Change Name and the look.
func TestAccount(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Alice")
	u, err := Create(dir, "Alice", "first-password")
	if err != nil {
		t.Fatal(err)
	}
	id := u.Profile.UserID

	// Change Password: the old one is needed; the ID stays.
	if err := u.ChangePassword("wrong-password", "second-password"); err == nil {
		t.Fatal("a wrong current password should be refused")
	}
	if err := u.ChangePassword("first-password", "short"); err == nil {
		t.Fatal("a short password should be refused")
	}
	if err := u.ChangePassword("first-password", "second-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := Unlock(dir, "first-password"); err != ErrWrongPassword {
		t.Fatal("the old password should stop working")
	}
	u2, err := Unlock(dir, "second-password")
	if err != nil || u2.Profile.UserID != id {
		t.Fatalf("unlock with the new password: %v", err)
	}

	// Test the Recovery Code.
	if _, err := Recover(dir, "AAAA", "third-password"); err == nil {
		t.Fatal("no code made yet")
	}
	code, err := u2.MakeRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 39 || strings.Count(code, "-") != 7 {
		t.Fatalf("code shape: %q", code)
	}
	if _, err := Recover(dir, strings.Repeat("A", 32), "third-password"); err == nil {
		t.Fatal("a wrong code should be refused")
	}
	u3, err := Recover(dir, strings.ToLower(code), "third-password")
	if err != nil || u3.Profile.UserID != id {
		t.Fatalf("recover: %v", err)
	}
	if _, err := Unlock(dir, "third-password"); err != nil {
		t.Fatal("the new password should work after recovering")
	}
	// The code still works afterwards (it's only replaced by a new one).
	if _, err := Recover(dir, code, "fourth-password"); err != nil {
		t.Fatal(err)
	}

	// Change Name, and the look.
	u4, _ := Unlock(dir, "fourth-password")
	if err := u4.SetName("  Alice B  "); err != nil || u4.Profile.DefaultName != "Alice B" {
		t.Fatalf("name: %v %q", err, u4.Profile.DefaultName)
	}
	if err := u4.SetLook(strings.Repeat("x", 191), nil, nil, false, false); err == nil {
		t.Fatal("bio too long")
	}
	if err := u4.SetLook("Pixel artist", []byte("pic"), []byte("ban"), false, false); err != nil {
		t.Fatal(err)
	}
	u5, _ := Unlock(dir, "fourth-password")
	if u5.Profile.Bio != "Pixel artist" || string(u5.Picture()) != "pic" || string(u5.BannerPicture()) != "ban" {
		t.Fatal("the look should be saved")
	}
	u5.SetLook("Pixel artist", nil, nil, false, true)
	if u5.BannerPicture() != nil || u5.Picture() == nil {
		t.Fatal("clearing the banner only")
	}
}
