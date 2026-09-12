package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSTRMAndTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "film.strm")
	if err := os.WriteFile(path, []byte("https://media.example/film.mp4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := STRM{root}
	result, err := s.Resolve(context.Background(), path)
	if err != nil || result.URL != "https://media.example/film.mp4" {
		t.Fatalf("%+v %v", result, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.strm")
	_ = os.WriteFile(outside, []byte("https://example.com/secret"), 0600)
	if _, err = s.Resolve(context.Background(), outside); err == nil {
		t.Fatal("allowed traversal")
	}
	entries, err := s.Scan(context.Background())
	if err != nil || len(entries) != 1 {
		t.Fatalf("%+v %v", entries, err)
	}
	_ = os.WriteFile(path, []byte("file:///etc/passwd"), 0600)
	if _, err = s.Resolve(context.Background(), path); err == nil {
		t.Fatal("allowed non-http source")
	}
}
func TestDirectValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "file:///a", "https://user:password@example.com/x", "https://example.com/\r\n"} {
		if ValidURL(raw) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
