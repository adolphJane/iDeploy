package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/databasebrowser"
)

func TestManagedDatabaseCommandsKeepCredentialsAndBackupsOffSSHArguments(t *testing.T) {
	for _, spec := range []struct {
		driver  databasebrowser.Driver
		version string
	}{
		{databasebrowser.DriverPostgres, "17"},
		{databasebrowser.DriverMySQL, "8.4"},
	} {
		t.Run(string(spec.driver), func(t *testing.T) {
			root := t.TempDir()
			docker := filepath.Join(root, "docker")
			owner := strings.Repeat("a", 32)
			fakeDocker := `#!/bin/sh
case "$1 $2" in
'container inspect'|'volume inspect') exit 1 ;;
'image inspect') printf 'sha256:fixture\n' ;;
'volume create') printf 'fixture-volume\n' ;;
'exec -i') test "$(cat)" = 'SELECT 1;' ;;
inspect*) printf '%s\n' ` + shellQuote(owner) + ` ;;
exec*)
	case "$*" in *pg_dump*|*mysqldump*) printf 'SELECT 1;\n' ;; esac ;;
esac
`
			if err := os.WriteFile(docker, []byte(fakeDocker), 0o700); err != nil {
				t.Fatal(err)
			}
			adminSecret := "synthetic-admin-fixture"
			appSecret := "synthetic-app-fixture"
			remote := &Remote{command: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				script := args[len(args)-1]
				if strings.Contains(script, adminSecret) || strings.Contains(script, appSecret) {
					t.Fatal("a credential was inserted into the SSH command")
				}
				script = strings.ReplaceAll(script, "$HOME/", root+"/")
				script = strings.ReplaceAll(script, "docker ", shellQuote(docker)+" ")
				return exec.CommandContext(ctx, "sh", "-c", script)
			}}
			instance := databasebrowser.Instance{
				ID: owner,
				InstanceConfig: databasebrowser.InstanceConfig{
					Name: "Database", Environment: "test", ServerID: "fixture", Driver: spec.driver,
					EngineVersion: spec.version, ListenIP: "10.0.0.3", Port: 8040, DatabaseName: "appdb", AppUsername: "app",
				},
			}
			ctx := context.Background()
			host := Connection{Host: "fixture.invalid", SSHUser: "fixture", Port: 22}
			if err := remote.DeployManagedDatabase(ctx, host, instance, adminSecret, appSecret); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, managedDatabaseDir(instance.ID))
			admin, err := os.ReadFile(filepath.Join(dir, "password"))
			if err != nil || string(admin) != adminSecret {
				t.Fatal("administrator credential file does not match input")
			}
			info, err := os.Stat(dir)
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatal("remote credential directory must be private")
			}
			id := strings.Repeat("b", 32)
			if err := remote.BackupManagedDatabase(ctx, host, instance, id); err != nil {
				t.Fatal(err)
			}
			backupPath := filepath.Join(dir, "backups", id+".sql")
			info, err = os.Stat(backupPath)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("backup file must be private")
			}
			if err := remote.RestoreManagedDatabase(ctx, host, instance.ID, id, instance); err != nil {
				t.Fatal(err)
			}
		})
	}
}
