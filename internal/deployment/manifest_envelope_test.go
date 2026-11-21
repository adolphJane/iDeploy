package deployment

import (
	"crypto/rand"
	"testing"
)

func TestManifestEnvelopeRoundTripAndTamper(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	manifest := `{"services":{"api":{"environment":{"TOKEN":"private-value"}}}}`
	envelope, err := sealManifestWithKey(key, manifest)
	if err != nil || envelope == "" || envelope == manifest {
		t.Fatalf("failed to seal manifest: %v", err)
	}
	plain, err := openManifestWithKey(key, envelope)
	if err != nil || plain != manifest {
		t.Fatalf("failed to open manifest: %v", err)
	}
	otherKey := make([]byte, 32)
	if _, err := rand.Read(otherKey); err != nil {
		t.Fatal(err)
	}
	if _, err := openManifestWithKey(otherKey, envelope); err == nil {
		t.Fatal("wrong key decrypted manifest")
	}
}
