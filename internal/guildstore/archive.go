package guildstore

// This file packs a guild folder into one file and unpacks it again. Used by
// export/import (encrypted) and backup/fall-back (not encrypted).

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// skipInArchive are folders inside a guild that are never packed: they only
// matter while the guild is running.
var skipInArchive = map[string]bool{RuntimeDir: true, LogsDir: true}

// WriteArchive packs the guild folder dir as a .tar.gz into w.
func WriteArchive(w io.Writer, dir string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		name := filepath.ToSlash(rel)
		if skipInArchive[strings.SplitN(name, "/", 2)[0]] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(path.Base(name), ".tmp-") {
			return nil // This skips a half-written temporary file.
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o700, ModTime: info.ModTime()})
		case info.Mode().IsRegular():
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.CopyN(tw, f, info.Size())
			return err
		default:
			return nil // Links and special files are not packed.
		}
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// ExtractArchive unpacks a .tar.gz from r into the empty folder dest.
func ExtractArchive(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errors.New("the file is not a guild archive or is damaged")
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("the file is damaged")
		}
		name := strings.TrimSuffix(h.Name, "/")
		// Never write outside dest, whatever the archive says.
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return fmt.Errorf("the file contains an unsafe path %q", h.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
	if !HasGuildData(dest) {
		return errors.New("the file does not contain a Nexus guild")
	}
	return nil
}

// --- Encrypted export files ---------------------------------------
//
// Layout: magic | 16-byte salt | chunks. Each chunk is a 4-byte length and
// AES-256-GCM sealed data (up to 64 KB of plain data). The nonce is the
// chunk number; the last chunk is marked so a cut-off file is detected.
// The key is made from the password with PBKDF2-SHA256 and the random salt.

const (
	exportMagic     = "NEXUS-GUILD-EXPORT-1\n"
	exportChunkSize = 64 * 1024
	exportKDFRounds = 600_000
)

func exportKey(password string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, exportKDFRounds, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunkNonce(n uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[4:], n)
	return b
}

type encryptWriter struct {
	w    io.Writer
	aead cipher.AEAD
	buf  []byte
	n    uint64
}

// NewEncryptWriter starts an export file on w, sealed with the guild's
// password: the file's mark and a random salt, then sealed chunks.
func NewEncryptWriter(w io.Writer, password string) (*encryptWriter, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := exportKey(password, salt)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, exportMagic); err != nil {
		return nil, err
	}
	if _, err := w.Write(salt); err != nil {
		return nil, err
	}
	return &encryptWriter{w: w, aead: aead}, nil
}

func (e *encryptWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		room := exportChunkSize - len(e.buf)
		take := min(room, len(p))
		e.buf = append(e.buf, p[:take]...)
		p = p[take:]
		if len(e.buf) == exportChunkSize {
			if err := e.flush(false); err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

func (e *encryptWriter) flush(last bool) error {
	flag := []byte{0}
	if last {
		flag[0] = 1
	}
	sealed := e.aead.Seal(nil, chunkNonce(e.n), e.buf, flag)
	e.n++
	e.buf = e.buf[:0]
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(sealed)))
	if _, err := e.w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := e.w.Write(sealed)
	return err
}

// Close writes the final chunk.
func (e *encryptWriter) Close() error { return e.flush(true) }

// ErrBadExportPassword means the export file's password was wrong.
var ErrBadExportPassword = errors.New("wrong password, or the file is damaged")

type decryptReader struct {
	r    io.Reader
	aead cipher.AEAD
	buf  []byte
	n    uint64
	done bool
}

// ReadExportHeader reads the start of an export file and returns the salt.
func ReadExportHeader(r io.Reader) ([]byte, error) {
	magic := make([]byte, len(exportMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != exportMagic {
		return nil, errors.New("this is not a Nexus guild export file")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, errors.New("the file is damaged")
	}
	return salt, nil
}

// NewDecryptReader opens the sealed chunks of an export file, after its
// header was read with ReadExportHeader.
func NewDecryptReader(r io.Reader, salt []byte, password string) (*decryptReader, error) {
	aead, err := exportKey(password, salt)
	if err != nil {
		return nil, err
	}
	d := &decryptReader{r: r, aead: aead}
	if err := d.next(); err != nil { // This checks the password right away.
		return nil, err
	}
	return d, nil
}

func (d *decryptReader) next() error {
	var hdr [4]byte
	if _, err := io.ReadFull(d.r, hdr[:]); err != nil {
		return errors.New("the file is cut off or damaged")
	}
	size := binary.BigEndian.Uint32(hdr[:])
	if size > exportChunkSize+uint32(d.aead.Overhead()) {
		return errors.New("the file is damaged")
	}
	sealed := make([]byte, size)
	if _, err := io.ReadFull(d.r, sealed); err != nil {
		return errors.New("the file is cut off or damaged")
	}
	for _, flag := range []byte{0, 1} {
		plain, err := d.aead.Open(nil, chunkNonce(d.n), sealed, []byte{flag})
		if err == nil {
			d.buf = plain
			d.n++
			d.done = flag == 1
			return nil
		}
	}
	return ErrBadExportPassword
}

func (d *decryptReader) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

// MoveFolderContents moves everything inside src into dst (both folders).
// It renames when possible and copies otherwise (e.g. across drives).
func MoveFolderContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, en := range entries {
		from, to := filepath.Join(src, en.Name()), filepath.Join(dst, en.Name())
		if err := os.Rename(from, to); err == nil {
			continue
		}
		if err := CopyTree(from, to); err != nil {
			return err
		}
	}
	return nil
}

// CopyTree copies a folder and everything in it.
func CopyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
