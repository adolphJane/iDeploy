package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLocalVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo","version":"1.2.3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := DetectLocalVersion(root, ".")
	if err != nil || value.Version != "1.2.3" || value.Source != "package.json" {
		t.Fatalf("unexpected suggestion: %#v, %v", value, err)
	}
}

func TestDetectLocalVersionContextCannotEscape(t *testing.T) {
	_, err := DetectLocalVersion(t.TempDir(), "../outside")
	if err == nil {
		t.Fatal("expected context escape to fail")
	}
}
