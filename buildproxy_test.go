package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/platform/database"
)

func TestValidateBuildProxySettingsNormalizesAndSortsRules(t *testing.T) {
	settings, err := validateBuildProxySettings(BuildProxySettingsInput{
		Mode: " REQUIRED ",
		Rules: []BuildProxyRule{
			{Upstream: "GHCR.IO", Mirror: "Harbor.Example.com/Proxy-GHCR/"},
			{Upstream: "docker.io/", Mirror: "harbor.example.com/proxy-dockerhub"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != "required" || len(settings.Rules) != 2 {
		t.Fatalf("unexpected settings: %#v", settings)
	}
	if settings.Rules[0].Upstream != "docker.io" || settings.Rules[1].Mirror != "harbor.example.com/proxy-ghcr" {
		t.Fatalf("rules were not normalized and sorted: %#v", settings.Rules)
	}
}

func TestValidateBuildProxySettingsRejectsUnsafeOrIncompleteRules(t *testing.T) {
	tests := []BuildProxySettingsInput{
		{Mode: "required"},
		{Mode: "required", Rules: []BuildProxyRule{{Upstream: "https://docker.io", Mirror: "harbor.example.com/proxy"}}},
		{Mode: "required", Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "https://harbor.example.com/proxy"}}},
		{Mode: "required", Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com"}}},
		{Mode: "required", Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com/proxy"}, {Upstream: "docker.io", Mirror: "harbor.example.com/other"}}},
	}
	for _, input := range tests {
		if _, err := validateBuildProxySettings(input); err == nil {
			t.Fatalf("expected invalid settings to fail: %#v", input)
		}
	}
}

func TestBuildProxyConfigUsesRegistryMirrorPaths(t *testing.T) {
	settings, err := validateBuildProxySettings(BuildProxySettingsInput{
		Mode:  "required",
		Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com/proxy-dockerhub"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := buildProxyConfig(settings)
	if !strings.Contains(config, `[registry."docker.io"]`) || !strings.Contains(config, `mirrors = ["harbor.example.com/proxy-dockerhub"]`) {
		t.Fatalf("unexpected BuildKit config:\n%s", config)
	}
	if first, second := buildProxyBuilderName(config), buildProxyBuilderName(config); first != second || !strings.HasPrefix(first, "ideploy-proxy-") {
		t.Fatalf("builder name is not stable: %q %q", first, second)
	}
}

func TestBuildProxySettingsPersistInAppSettings(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "ideploy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings := BuildProxySettingsInput{Mode: "preferred", Rules: []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com/proxy"}}}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES (?,?,?)`, buildProxySettingsKey, string(encoded), "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBuildProxySettings(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != "preferred" || len(loaded.Rules) != 1 || loaded.Rules[0].Mirror != "harbor.example.com/proxy" {
		t.Fatalf("unexpected persisted settings: %#v", loaded)
	}
}
