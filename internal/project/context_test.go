package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIProjectSummaryRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo","token":"do-not-send"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := AIProjectSummary(root, ".")
	if err != nil || !strings.Contains(summary, "REDACTED") || strings.Contains(summary, "do-not-send") {
		t.Fatalf("summary was not redacted: %q, %v", summary, err)
	}
}
