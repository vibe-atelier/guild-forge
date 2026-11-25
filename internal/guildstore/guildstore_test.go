package guildstore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gd, err := NewGuildData("goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	if err := gd.Save(dir); err != nil {
		t.Fatal(err)
	}
	if !HasGuildData(dir) {
		t.Fatal("folder should be a guild now")
	}
	back, err := LoadGuildData(dir)
	if err != nil || back.PublicKey != gd.PublicKey || !back.CheckPassword("goodpassword") || back.CheckPassword("nope") {
		t.Fatalf("got %+v, %v", back, err)
	}
	if len(gd.PublicKey) != 52 {
		t.Fatalf("guild ID should be 52 characters, got %d", len(gd.PublicKey))
	}
}

func TestArchiveRoundTripSkipsRuntimeAndLogs(t *testing.T) {
	src := t.TempDir()
	gd, _ := NewGuildData("goodpassword")
	gd.Save(src)
	os.MkdirAll(filepath.Join(src, "media"), 0o700)
	os.WriteFile(filepath.Join(src, "media", "a.bin"), []byte("hello"), 0o600)
	os.MkdirAll(filepath.Join(src, LogsDir), 0o700)
	os.WriteFile(filepath.Join(src, LogsDir, "guild.log"), []byte("log"), 0o600)
	os.MkdirAll(filepath.Join(src, RuntimeDir), 0o700)
	os.WriteFile(filepath.Join(src, RuntimeDir, "control.json"), []byte("{}"), 0o600)

	var buf bytes.Buffer
	if err := WriteArchive(&buf, src); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := ExtractArchive(&buf, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "media", "a.bin")); string(b) != "hello" {
		t.Fatal("media not restored")
	}
	for _, skipped := range []string{LogsDir, RuntimeDir} {
		if _, err := os.Stat(filepath.Join(dst, skipped)); err == nil {
			t.Fatalf("%s should not be packed", skipped)
		}
	}
}

// An archive that tries to write outside its folder is refused.
func TestArchiveRejectsUnsafePaths(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	data := []byte("evil")
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "../escaped.txt", Mode: 0o600, Size: int64(len(data))})
	tw.Write(data)
	tw.Close()
	gz.Close()

	parent := t.TempDir()
	dst := filepath.Join(parent, "guild")
	os.Mkdir(dst, 0o700)
	if err := ExtractArchive(&buf, dst); err == nil {
		t.Fatal("an unsafe path must be refused")
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Fatal("a file was written outside the folder")
	}
}

func TestExportEncryption(t *testing.T) {
	var file bytes.Buffer
	w, err := NewEncryptWriter(&file, "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("nexus "), 30_000) // This makes more than one 64 KB chunk.
	w.Write(plain)
	w.Close()
	if bytes.Contains(file.Bytes(), []byte("nexus nexus")) {
		t.Fatal("the export must be encrypted")
	}

	open := func(pw string, data []byte) ([]byte, error) {
		r := bytes.NewReader(data)
		salt, err := ReadExportHeader(r)
		if err != nil {
			return nil, err
		}
		d, err := NewDecryptReader(r, salt, pw)
		if err != nil {
			return nil, err
		}
		return io.ReadAll(d)
	}
	got, err := open("goodpassword", file.Bytes())
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %v", err)
	}
	if _, err := open("wrongpassword", file.Bytes()); !errors.Is(err, ErrBadExportPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	// A cut-off file is detected, not silently accepted.
	if _, err := open("goodpassword", file.Bytes()[:file.Len()-100]); err == nil {
		t.Fatal("a cut-off file must be refused")
	}
}
