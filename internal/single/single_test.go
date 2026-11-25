package single

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaim(t *testing.T) {
	dir := t.TempDir()
	already, wake, stop := Claim(dir)
	if already || wake == nil {
		t.Fatal("the first copy runs")
	}
	if again, _, _ := Claim(dir); !again {
		t.Fatal("a second copy finds the first")
	}
	select {
	case <-wake:
	case <-time.After(2 * time.Second):
		t.Fatal("the first copy is asked to show itself")
	}
	stop()
	// A file left by a copy that stopped doesn't count.
	os.WriteFile(filepath.Join(dir, FileName()), []byte("127.0.0.1:1 nothing\n"), 0o600)
	if again, _, stop2 := Claim(dir); again {
		t.Fatal("a stale file is ignored")
	} else {
		stop2()
	}
}
