package registry_test

import (
	"context"
	"path/filepath"
	"testing"

	"ideploy/internal/platform/database"
	"ideploy/internal/registry"
)

func TestStoreCreatesListsAndUpdatesConnection(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	store := registry.NewStore(db)
	created, err := store.Create(ctx, registry.NewConnection{
		Name:     "Local Harbor",
		Kind:     registry.KindHarbor,
		Endpoint: "harbor.local",
		Username: "robot",
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	if created.Endpoint != "https://harbor.local" || created.Status != registry.StatusUnknown || created.Username != "robot" {
		t.Fatalf("unexpected created connection: %#v", created)
	}

	updated, err := store.UpdateProbe(ctx, created.ID, registry.StatusHealthy, "Harbor API 可用")
	if err != nil {
		t.Fatalf("update probe: %v", err)
	}
	if updated.LastCheckedAt == nil || updated.Status != registry.StatusHealthy {
		t.Fatalf("unexpected updated connection: %#v", updated)
	}

	connections, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(connections) != 1 || connections[0].ID != created.ID {
		t.Fatalf("unexpected connections: %#v", connections)
	}
}
