package server

import "testing"

func TestNewConnectionValidate(t *testing.T) {
	value, err := (NewConnection{Name: "production", Host: "10.0.0.8", SSHUser: "deploy"}).Validate()
	if err != nil || value.Port != 22 {
		t.Fatalf("validate server: %#v, %v", value, err)
	}
	if _, err := (NewConnection{Name: "bad", Host: "https://host", SSHUser: "deploy"}).Validate(); err == nil {
		t.Fatal("expected scheme validation error")
	}
	if _, err := (NewConnection{Name: "bad", Host: "host\nother", SSHUser: "deploy"}).Validate(); err == nil {
		t.Fatal("expected newline host validation error")
	}
	if _, err := (NewConnection{Name: "bad", Host: "host", SSHUser: "-oProxyCommand=bad"}).Validate(); err == nil {
		t.Fatal("expected SSH option-like user validation error")
	}
}

func TestValidContainerID(t *testing.T) {
	if !validContainerID("api.1") || validContainerID("api; rm -rf /") {
		t.Fatal("container identifier validation failed")
	}
}

func TestValidateAuthMethod(t *testing.T) {
	if err := ValidateAuthMethod(AuthMethodAgent, "", ""); err != nil {
		t.Fatalf("agent auth should not require a password: %v", err)
	}
	if err := ValidateAuthMethod(AuthMethodPassword, "", ""); err == nil {
		t.Fatal("password auth should require a password")
	}
	if err := ValidateAuthMethod(AuthMethodPassword, "secret", ""); err != nil {
		t.Fatalf("password auth rejected valid password: %v", err)
	}
	if err := ValidateAuthMethod(AuthMethodPrivateKey, "", ""); err == nil {
		t.Fatal("private-key auth should require a key path")
	}
	if err := ValidateAuthMethod(AuthMethodPrivateKey, "", "/tmp/id_ed25519"); err != nil {
		t.Fatalf("private-key auth rejected a key path: %v", err)
	}
}
