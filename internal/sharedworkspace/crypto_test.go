package sharedworkspace

import (
	"testing"
)

func TestVaultKeyAndSecretBinding(t *testing.T) {
	config, key, err := CreateVault()
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := VaultKey(config)
	if err != nil || string(recovered) != string(key) {
		t.Fatalf("vault key load failed: %v", err)
	}
	envelope, err := EncryptSecret(key, "server:one", "private value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptSecret(key, "server:two", envelope); err == nil {
		t.Fatal("secret was accepted under different id")
	}
	value, err := DecryptSecret(key, "server:one", envelope)
	if err != nil || value != "private value" {
		t.Fatalf("secret round trip failed: %v", err)
	}
}
