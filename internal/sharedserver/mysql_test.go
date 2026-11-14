package sharedserver

import (
	"context"
	"strings"
	"testing"
)

func TestMySQLRequiresTLSAndDatabase(t *testing.T) {
	for _, dsn := range []string{
		"user:pass@tcp(example.com:3306)/ideploy_shared?tls=false",
		"user:pass@tcp(example.com:3306)/?tls=true",
	} {
		_, err := OpenWithDriver(context.Background(), "mysql", dsn, strings.Repeat("x", 32))
		if err == nil || !strings.Contains(err.Error(), "encrypted TLS") {
			t.Fatalf("accepted unsafe DSN %q: %v", dsn, err)
		}
	}
	if err := validateMySQLDSN("user:pass@tcp(example.com:3306)/ideploy_shared?tls=skip-verify"); err != nil {
		t.Fatalf("rejected encrypted MySQL DSN without certificate verification: %v", err)
	}
	if err := validateMySQLDSN("user:pass@tcp(example.com:3306)/ideploy_shared?tls=preferred"); err == nil {
		t.Fatal("accepted TLS mode that can fall back to plaintext")
	}
	server := &Server{driver: "mysql"}
	if got := server.query("WHERE kind=$1 AND id=$2"); got != "WHERE kind=? AND id=?" {
		t.Fatalf("unexpected placeholders: %s", got)
	}
}
