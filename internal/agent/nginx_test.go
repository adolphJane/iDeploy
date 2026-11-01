package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNginxApplyRestoresConfigWhenValidationFails(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "nginx.conf")
	previous := "events {}\nhttp { server { listen 80; } }\n"
	if err := os.WriteFile(config, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = exec ] && [ "$4" = -v ]; then echo 'nginx version: nginx/1.27'; exit 0; fi
if [ "$1" = inspect ]; then echo "host|bind:$NGINX_CONFIG"; exit 0; fi
if [ "$1" = exec ] && [ "$4" = -t ]; then
  if grep -q invalid_route "$NGINX_CONFIG"; then exit 1; fi
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NGINX_CONFIG", config)
	oldBackup := nginxBackupDir
	nginxBackupDir = filepath.Join(dir, "backups")
	t.Cleanup(func() { nginxBackupDir = oldBackup })
	hash := sha256.Sum256([]byte(previous))
	req := OperationRequest{ID: strings.Repeat("f", 32), Kind: "nginx.apply", NginxContainer: "dev-nginx", NginxConfig: previous + "# invalid_route\n", ExpectedHash: fmt.Sprintf("%x", hash[:])}
	if _, err := applyNginx(t.Context(), req); err == nil {
		t.Fatal("invalid config was applied")
	}
	current, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != previous {
		t.Fatalf("config was not restored: %q", current)
	}
	backup, err := os.ReadFile(filepath.Join(nginxBackupDir, req.ID+".conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != previous {
		t.Fatal("backup differs from original config")
	}
}
