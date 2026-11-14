package databasebrowser

import (
	"context"
	"testing"
)

func TestCreateDatabaseRejectsUnsafeNameBeforeConnecting(t *testing.T) {
	for _, name := range []string{"ifx; DROP DATABASE other", "1starts_with_number", "with-dash", ""} {
		if err := CreateDatabase(context.Background(), Connection{Driver: DriverMySQL}, "unused", name); err == nil {
			t.Fatalf("accepted invalid database name %q", name)
		}
	}
}
