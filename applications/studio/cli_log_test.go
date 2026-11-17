package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLogTailsProjectServiceFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"go.mod", "foundation/cmd/api/main.go"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	project := projectRef{Root: root}
	log, err := readLog(project, "backend")
	if err != nil || log != "" {
		t.Fatalf("missing log: got %q, %v", log, err)
	}

	dir := filepath.Join(root, ".local", "horizon-cli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tail := strings.Repeat("x", 24_000)
	if err := os.WriteFile(filepath.Join(dir, "backend.log"), []byte("previous"+tail), 0600); err != nil {
		t.Fatal(err)
	}
	log, err = readLog(project, "backend")
	if err != nil || log != tail {
		t.Fatalf("wanted final 24000 bytes, got %d and %v", len(log), err)
	}

	t.Run("rejects symlink", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(dir, "backend.log"), filepath.Join(dir, "frontend.log")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := readLog(project, "frontend"); err == nil {
			t.Fatal("unexpectedly followed a symlink")
		}
	})
}
