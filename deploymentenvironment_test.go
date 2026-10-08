package main

import (
	"encoding/json"
	"strings"
	"testing"

	"ideploy/internal/deployment"
)

func TestDeploymentEnvironmentViewNeverReturnsSecretValue(t *testing.T) {
	view := deploymentEnvironmentView(deployment.Target{ID: "deployment-1", EnvironmentRevision: 2, AppliedEnvironmentRevision: 1}, []deployment.EnvironmentVariable{
		{Name: "DB_MODE", Kind: deployment.EnvironmentKindPlain, PlainValue: "mysql"},
		{Name: "DATABASE_PASSWORD", Kind: deployment.EnvironmentKindSecret, PlainValue: "must-not-leak", CredentialRef: deployment.EnvironmentSecretCredentialRef("secret-1")},
	})
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !view.PendingApply || !strings.Contains(string(encoded), `"value":"mysql"`) || strings.Contains(string(encoded), "must-not-leak") {
		t.Fatalf("environment view was not sanitized: %s", encoded)
	}
	if !view.Variables[1].Configured || view.Variables[1].Value != "" {
		t.Fatalf("unexpected secret view: %#v", view.Variables[1])
	}
}
