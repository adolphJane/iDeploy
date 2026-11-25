package deployment_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/databasebrowser"
	"ideploy/internal/deployment"
	"ideploy/internal/platform/database"
)

func TestEnvironmentSeparatesTargetsDatabasesAndReleaseArtifacts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "environments.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, fixture := range []string{
		`INSERT INTO projects(id,name,created_at,updated_at)
		  VALUES('p','API','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO servers(id,name,host,ssh_user,created_at,updated_at) VALUES
		  ('prod','Production','prod.example','deploy','x','x'),
		  ('alias','Alias','PROD.EXAMPLE.','other','x','x'),
		  ('test','Test','test.example','deploy','x','x')`,
	} {
		if _, err := db.ExecContext(ctx, fixture); err != nil {
			t.Fatal(err)
		}
	}
	targets := deployment.NewStore(db)
	databases := databasebrowser.NewStore(db)
	prodDB, err := databases.Create(ctx, databasebrowser.NewConnection{
		Name: "Production", Environment: "production", Driver: databasebrowser.DriverPostgres,
		Host: "prod.example", DatabaseName: "api", Username: "browser",
	})
	if err != nil {
		t.Fatal(err)
	}
	testDB, err := databases.Create(ctx, databasebrowser.NewConnection{
		Name: "Test", Environment: "test", Driver: databasebrowser.DriverPostgres,
		Host: "test.example", DatabaseName: "api", Username: "browser",
	})
	if err != nil {
		t.Fatal(err)
	}
	production, err := targets.Create(ctx, deployment.Input{ProjectID: "p", ServerID: "prod", WorkloadKind: "container", WorkloadName: "api"})
	if err != nil || production.Environment != "default" {
		t.Fatalf("existing configuration default: %+v, %v", production, err)
	}
	legacy, err := targets.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: production.ID, Version: "1.0.0"})
	if err != nil || legacy.ArtifactTag() != "1.0.0" {
		t.Fatalf("legacy release: %+v, %v", legacy, err)
	}
	if err := targets.Configure(ctx, production.ID, deployment.Input{Environment: "production",
		WorkloadKind: "container", WorkloadName: "api", DatabaseConnectionID: &prodDB.ID}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []deployment.Input{
		{ProjectID: "p", ServerID: "prod", Environment: "test"},
		{ProjectID: "p", ServerID: "alias", Environment: "test"},
		{ProjectID: "p", ServerID: "test", Environment: "production"},
		{ProjectID: "p", ServerID: "test", Environment: "test", DatabaseConnectionID: &prodDB.ID},
	} {
		input.WorkloadKind, input.WorkloadName = "container", "api"
		if _, err := targets.Create(ctx, input); err == nil {
			t.Fatalf("accepted conflicting target: %+v", input)
		}
	}
	testTarget, err := targets.Create(ctx, deployment.Input{ProjectID: "p", ServerID: "test", Environment: "test",
		WorkloadKind: "container", WorkloadName: "api", DatabaseConnectionID: &testDB.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := databases.Delete(ctx, prodDB.ID); err == nil {
		t.Fatal("deleted a bound database connection")
	}
	if err := targets.Configure(ctx, testTarget.ID, deployment.Input{WorkloadKind: "container", WorkloadName: "api"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := targets.Get(ctx, testTarget.ID)
	if err != nil || reloaded.Environment != "test" || reloaded.DatabaseConnectionID != testDB.ID {
		t.Fatalf("legacy edit lost environment bindings: %+v, %v", reloaded, err)
	}
	version := "1.1.0-" + strings.Repeat("v", 122)
	prodRelease, err := targets.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: production.ID, Version: version})
	if err != nil {
		t.Fatal(err)
	}
	testRelease, err := targets.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: testTarget.ID, Version: version})
	if err != nil || prodRelease.ArtifactTag() == testRelease.ArtifactTag() || len(testRelease.ArtifactTag()) > 128 {
		t.Fatalf("release tag collision: %q vs %q, %v", prodRelease.ArtifactTag(), testRelease.ArtifactTag(), err)
	}
	if _, err := targets.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: production.ID, Action: "rollback",
		Version: version, RollbackToID: testRelease.ID}); err == nil {
		t.Fatal("accepted rollback from another target")
	}
	if err := targets.UpdateRelease(ctx, testRelease.ID, deployment.ReleaseFailed, "fixture", "", "", ""); err != nil {
		t.Fatal(err)
	}
	retry, err := targets.CreateRelease(ctx, deployment.ReleaseInput{DeploymentID: testTarget.ID, Version: version,
		PolicySourceID: testRelease.ID, AllowRetry: true})
	if err != nil || retry.ArtifactTag() != testRelease.ArtifactTag() {
		t.Fatalf("retry changed pinned artifact tag: %+v, %v", retry, err)
	}
	source, err := targets.GetRelease(ctx, legacy.ID)
	if err != nil || source.ArtifactTag() != "1.0.0" || source.DeploymentID != production.ID {
		t.Fatalf("assignment lost legacy release: %+v, %v", source, err)
	}
}
