package main

import (
	"strings"
	"testing"
)

func TestPinPushAndRejectLegacyRollback(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	ref, err := pinnedPushReference("harbor.example/team/api", "waiting\n1.0.0: digest: "+digest+" size: 123")
	if err != nil || ref != "harbor.example/team/api@"+digest {
		t.Fatalf("%s %v", ref, err)
	}
	if _, err := pinnedPushReference("api", "push finished without digest"); err == nil {
		t.Fatal("accepted missing digest")
	}
	if err := validateRollbackArtifact("api:1.0.0", ""); err == nil {
		t.Fatal("accepted mutable rollback")
	}
	if err := validateRollbackArtifact(ref, ""); err != nil {
		t.Fatal(err)
	}
}

func TestPinComposeSnapshot(t *testing.T) {
	config := `{"services":{"api":{"image":"host/team/api@sha256:` + strings.Repeat("b", 64) + `","build":{"context":"/local/path"},"environment":{"PASSWORD":"literal$value"}}}}`
	manifest, err := pinnedComposeManifest(config)
	if err != nil || strings.Contains(manifest, `"build"`) || !strings.Contains(manifest, "literal$$value") {
		t.Fatalf("%s %v", manifest, err)
	}
	if err := validateRollbackArtifact("compose:api:1.0.0", manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := pinnedComposeManifest(`{"services":{"api":{"image":"api:latest"}}}`); err == nil {
		t.Fatal("accepted mutable compose service")
	}
}
