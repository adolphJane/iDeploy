package sharedworkspace_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/platform/database"
	"ideploy/internal/sharedworkspace"
)

func TestExportAndMaterializeKeepDevicePathsLocal(t *testing.T) {
	ctx := context.Background()
	source, err := database.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.ExecContext(ctx, `INSERT INTO projects(id,name,created_at,updated_at) VALUES('p1','Example','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := source.ExecContext(ctx, `INSERT INTO project_sources(project_id,kind,local_path) VALUES('p1','local','/Users/first/private/project')`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sharedworkspace.Export(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Preview.Counts["projects"] != 1 || snapshot.Preview.Counts["project_sources"] != 1 || len(snapshot.Preview.LocalPaths) != 1 {
		t.Fatalf("unexpected preview: %+v", snapshot.Preview)
	}
	for _, record := range snapshot.Records {
		if strings.Contains(string(record.Data), "/Users/first") || strings.Contains(string(record.Data), "credential_ref") {
			t.Fatalf("device-only field leaked: %s", record.Data)
		}
	}
	target, err := database.Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := target.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,mode) VALUES(1,'server','shared')`); err != nil {
		t.Fatal(err)
	}
	changes := make([]sharedworkspace.Change, 0, len(snapshot.Records))
	for index, item := range snapshot.Records {
		changes = append(changes, sharedworkspace.Change{Sequence: int64(index + 1), Kind: item.Kind, ID: item.ID, Revision: 1, Data: item.Data})
	}
	if err := sharedworkspace.ApplyChanges(ctx, target, changes, int64(len(changes))); err != nil {
		t.Fatal(err)
	}
	var name, path string
	if err := target.QueryRowContext(ctx, `SELECT projects.name,project_sources.local_path FROM projects JOIN project_sources ON projects.id=project_sources.project_id WHERE projects.id='p1'`).Scan(&name, &path); err != nil {
		t.Fatal(err)
	}
	if name != "Example" || path != "" {
		t.Fatalf("unexpected restored project %q, path %q", name, path)
	}
}

func TestExportGitCredentialAsSecretReference(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO projects(id,name,created_at,updated_at) VALUES('p1','Private','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_sources(project_id,kind,repository_url,credential_ref) VALUES('p1','git','https://example.com/private.git','keychain:git-project:p1')`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sharedworkspace.Export(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Preview.Blockers) != 0 || len(snapshot.Secrets) != 1 || snapshot.Secrets[0].ID != "r.project_sources:p1:credential" {
		t.Fatalf("unexpected export: %#v", snapshot.Preview)
	}
	for _, record := range snapshot.Records {
		if strings.Contains(string(record.Data), "keychain:") {
			t.Fatalf("credential reference leaked in resource: %s", record.Data)
		}
	}
}

func TestExportBuildSecretMetadataWithoutValue(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('p1','API','2026-01-01','2026-01-01')`,
		`INSERT INTO servers(id,name,host,port,ssh_user,created_at,updated_at) VALUES('s1','Server','example.com',22,'root','2026-01-01','2026-01-01')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,environment_revision,created_at,updated_at) VALUES('d1','p1','s1',4,'2026-01-01','2026-01-01')`,
		`INSERT INTO deployment_environment_variables(id,deployment_id,name,kind,credential_ref,build_secret,created_at,updated_at) VALUES('v1','d1','PACKAGE_TOKEN','secret','keychain:deployment-environment:v1',1,'2026-01-01','2026-01-01')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := sharedworkspace.Export(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Secrets) != 1 || snapshot.Secrets[0].ID != "r.env:v1:credential" {
		t.Fatalf("secret export: %#v", snapshot.Secrets)
	}
	var found bool
	for _, record := range snapshot.Records {
		if record.Kind != "r.env" {
			continue
		}
		found = true
		if !strings.Contains(string(record.Data), `"build_secret":1`) || strings.Contains(string(record.Data), "keychain:") {
			t.Fatalf("metadata leaked or missing: %s", record.Data)
		}
	}
	if !found {
		t.Fatal("environment metadata missing")
	}
}

func TestInternalIngressAndPublishIntentSurviveSharedMaterialization(t *testing.T) {
	ctx := context.Background()
	source, err := database.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for _, statement := range []string{
		`INSERT INTO projects(id,name,created_at,updated_at) VALUES('project','Example','2026-01-01','2026-01-01')`,
		`INSERT INTO servers(id,name,host,port,ssh_user,created_at,updated_at) VALUES('server','Server','example.com',22,'root','2026-01-01','2026-01-01')`,
		`INSERT INTO deployment_targets(id,project_id,server_id,created_at,updated_at) VALUES('deployment','project','server','2026-01-01','2026-01-01')`,
		`INSERT INTO deployment_publish_paths(deployment_id,fqdn,path_prefix,service_name,container_port,updated_at) VALUES('deployment','apps.example.com','demo','api',8080,'2026-01-01')`,
		`INSERT INTO deployment_releases(id,deployment_id,version,status,created_at) VALUES('release','deployment','v1','failed','2026-01-01')`,
		`INSERT INTO release_path_intents(release_id,fqdn,path_prefix,service_name,container_port) VALUES('release','apps.example.com','demo','api',8080)`,
		`INSERT INTO internal_ingresses(fqdn,server_id,gateway_port,certificate_path,certificate_key_path,webroot,status,updated_at) VALUES('apps.example.com','server',8766,'/etc/cert.pem','/etc/key.pem','/srv/acme','ready','2026-01-01')`,
	} {
		if _, err := source.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := sharedworkspace.Export(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"r.internal_ingresses", "r.publish_paths", "r.release_path_intents"} {
		found := false
		for _, record := range snapshot.Records {
			if record.Kind == kind {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in shared snapshot", kind)
		}
	}
	target, err := database.Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := target.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,mode) VALUES(1,'server','shared')`); err != nil {
		t.Fatal(err)
	}
	changes := make([]sharedworkspace.Change, 0, len(snapshot.Records))
	for index, item := range snapshot.Records {
		changes = append(changes, sharedworkspace.Change{Sequence: int64(index + 1), Kind: item.Kind, ID: item.ID, Revision: 1, Data: item.Data})
	}
	if err := sharedworkspace.ApplyChanges(ctx, target, changes, int64(len(changes))); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM internal_ingresses WHERE fqdn='apps.example.com' AND status='ready' AND server_id='server'`,
		`SELECT COUNT(*) FROM deployment_publish_paths WHERE deployment_id='deployment' AND path_prefix='demo'`,
		`SELECT COUNT(*) FROM release_path_intents WHERE release_id='release' AND container_port=8080`,
	} {
		var count int
		if err := target.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("materialization failed: %s, count=%d, err=%v", query, count, err)
		}
	}
}

func TestJoinPreservesBootstrapSSHPrivateKeyPath(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "join.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_user,auth_method,private_key_path,created_at,updated_at)
		VALUES('temporary','Server','example.com',22,'root','private_key','/Users/second/.ssh/id_ed25519','2026-01-01','2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	change := sharedworkspace.Change{Sequence: 1, Kind: "r.servers", ID: "canonical", Revision: 1,
		Data: []byte(`{"id":"canonical","name":"Server","host":"example.com","port":22,"ssh_user":"root","auth_method":"private_key","created_at":"2026-01-01","updated_at":"2026-01-01"}`)}
	if err := sharedworkspace.JoinChanges(ctx, db, []sharedworkspace.Change{change}, 1, "temporary", "canonical", "device"); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := db.QueryRowContext(ctx, `SELECT private_key_path FROM servers WHERE id='canonical'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != "/Users/second/.ssh/id_ed25519" {
		t.Fatalf("private key path lost: %q", path)
	}
}

func TestTwoDeviceChangeAndDeleteMaterialization(t *testing.T) {
	ctx := context.Background()
	first, err := database.Open(ctx, filepath.Join(t.TempDir(), "first.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := database.Open(ctx, filepath.Join(t.TempDir(), "second.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, db := range []*sql.DB{first, second} {
		if _, err := db.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,mode) VALUES(1,'server','shared')`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.ExecContext(ctx, `INSERT INTO projects(id,name,created_at,updated_at) VALUES('p','Initial','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sharedworkspace.Export(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Records) != 1 {
		t.Fatalf("expected one record, got %d", len(snapshot.Records))
	}
	initial := sharedworkspace.Change{Sequence: 1, Kind: snapshot.Records[0].Kind, ID: "p", Revision: 1, Data: snapshot.Records[0].Data}
	for _, db := range []*sql.DB{first, second} {
		if err := sharedworkspace.ApplyChanges(ctx, db, []sharedworkspace.Change{initial}, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := second.ExecContext(ctx, `UPDATE projects SET name='Changed' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = sharedworkspace.Export(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := sharedworkspace.Diff(ctx, second, snapshot)
	if err != nil || len(mutations) != 1 || mutations[0].ExpectedRevision != 1 {
		t.Fatalf("unexpected update: %+v, %v", mutations, err)
	}
	update := sharedworkspace.Change{Sequence: 2, Kind: "r.projects", ID: "p", Revision: 2, Data: mutations[0].Data}
	for _, db := range []*sql.DB{first, second} {
		if err := sharedworkspace.ApplyChanges(ctx, db, []sharedworkspace.Change{update}, 2); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.ExecContext(ctx, `DELETE FROM projects WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = sharedworkspace.Export(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err = sharedworkspace.Diff(ctx, first, snapshot)
	if err != nil || len(mutations) != 1 || !mutations[0].Deleted || mutations[0].ExpectedRevision != 2 {
		t.Fatalf("unexpected tombstone: %+v, %v", mutations, err)
	}
	deletion := sharedworkspace.Change{Sequence: 3, Kind: "r.projects", ID: "p", Revision: 3, Deleted: true, Data: []byte(`{}`)}
	for _, db := range []*sql.DB{first, second} {
		if err := sharedworkspace.ApplyChanges(ctx, db, []sharedworkspace.Change{deletion}, 3); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id='p'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("delete not applied: %d, %v", count, err)
		}
	}
}

func TestDatabaseProbeTimestampDoesNotCreateSharedChange(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO shared_sync_state(id,server_id,mode) VALUES(1,'server','shared')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO database_connections(id,name,driver,host,port,database_name,username,created_at,updated_at) VALUES('db','Example','mysql','127.0.0.1',3306,'example','user','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sharedworkspace.Export(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Records) != 1 {
		t.Fatalf("expected one record, got %d", len(snapshot.Records))
	}
	change := sharedworkspace.Change{Sequence: 1, Kind: "r.databases", ID: "db", Revision: 1, Data: snapshot.Records[0].Data}
	if err := sharedworkspace.ApplyChanges(ctx, db, []sharedworkspace.Change{change}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE database_connections SET updated_at='2026-01-02',last_connected_at='2026-01-02' WHERE id='db'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = sharedworkspace.Export(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := sharedworkspace.Diff(ctx, db, snapshot)
	if err != nil || len(changes) != 0 {
		t.Fatalf("probe generated shared change: %+v, %v", changes, err)
	}
}
