package buildexecutor

import (
	"context"
	"ideploy/internal/releaseplan"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeBuildImagesAndPinnedSnapshot(t *testing.T) {
	config := `{"services":{"api":{"image":"registry.example:5000/team/api:1.0.0","platform":"linux/amd64","build":{"context":"."}},"db":{"image":"postgres:17"}}}`
	images, err := ComposeBuildImages(config, "linux/amd64", "registry.example:5000")
	if err != nil || len(images) != 1 || images[0] != "registry.example:5000/team/api:1.0.0" {
		t.Fatalf("images: %v %v", images, err)
	}
	if _, err := ComposeBuildImages(config, "linux/arm64", "registry.example:5000"); err == nil {
		t.Fatal("accepted mismatched platform")
	}
	if _, err := ComposeBuildImages(config, "linux/amd64", "other.example"); err == nil {
		t.Fatal("accepted wrong registry")
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	resolved := `{"services":{"api":{"image":"registry.example:5000/team/api@` + digest + `","build":{"context":"."}},"db":{"image":"postgres@` + digest + `"}}}`
	manifest, err := PinComposeManifest(resolved)
	if err != nil || strings.Contains(manifest, `"build"`) || !strings.Contains(manifest, digest) {
		t.Fatalf("manifest: %s %v", manifest, err)
	}
	if _, err := PinComposeManifest(config); err == nil {
		t.Fatal("accepted mutable tags")
	}
}

func TestComposeBuildSecretsAreMappedAndRemovedFromRuntimeSnapshot(t *testing.T) {
	raw := []byte(`{"services":{"api":{"build":{"secrets":[{"source":"PACKAGE_TOKEN"}]},"image":"registry.example/api@sha256:` + strings.Repeat("a", 64) + `"}},"secrets":{"PACKAGE_TOKEN":{"environment":"PACKAGE_TOKEN"}}}`)
	if err := validateComposeBuildSecrets(raw, map[string]string{"PACKAGE_TOKEN": "private-value"}); err != nil {
		t.Fatal(err)
	}
	if err := validateComposeBuildSecrets(raw, nil); err == nil {
		t.Fatal("accepted unconfigured build secret")
	}
	if err := ensureComposeSecretsNotExpanded(raw, map[string]string{"PACKAGE_TOKEN": "private-value"}); err != nil {
		t.Fatal(err)
	}
	leaked := []byte(`{"services":{"api":{"environment":{"TOKEN":"private-value"}}}}`)
	if err := ensureComposeSecretsNotExpanded(leaked, map[string]string{"PACKAGE_TOKEN": "private-value"}); err == nil {
		t.Fatal("accepted expanded build secret")
	}
	clean, err := stripComposeBuildSecrets(raw)
	if err != nil || strings.Contains(string(clean), `"secrets":{`) {
		t.Fatalf("snapshot retained build secret source: %s %v", clean, err)
	}
	fileSecret := []byte(`{"services":{"api":{"build":{"secrets":[{"source":"PACKAGE_TOKEN"}]}}},"secrets":{"PACKAGE_TOKEN":{"file":"/host/private"}}}`)
	if err := validateComposeBuildSecrets(fileSecret, map[string]string{"PACKAGE_TOKEN": "private-value"}); err == nil {
		t.Fatal("accepted host file build secret")
	}
	runtimeSecret := []byte(`{"services":{"api":{"secrets":[{"source":"PACKAGE_TOKEN"}]}}}`)
	if _, err := stripComposeBuildSecrets(runtimeSecret); err == nil {
		t.Fatal("accepted unsupported runtime secret")
	}
}

func TestBuildComposePushesAndPinsWithoutLocalDocker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "compose.yml"), []byte("services: {} # ${IMAGE_TAG}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = login ]; then read password; [ "$password" = registry-password ] || exit 2; exit 0; fi
[ "$1" = compose ] || exit 3
case "$*" in
  *--resolve-image-digests*) printf '{"services":{"api":{"image":"registry.example/team/api@sha256:%s","build":{"context":"%s"}}}}' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$FAKE_ROOT" ;;
  *config*) printf '{"services":{"api":{"image":"registry.example/team/api:1.0.0","platform":"linux/amd64","build":{"context":"%s","dockerfile":"Dockerfile"}}}}' "$FAKE_ROOT" ;;
  *build*) exit 0 ;;
  *) exit 4 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ROOT", root)
	doc := releaseplan.Document{SchemaVersion: 1, DeploymentID: strings.Repeat("a", 32), ReleaseID: strings.Repeat("b", 32), SourceCommit: strings.Repeat("c", 40), WorkloadKind: "compose", ContextPath: ".", ComposeFile: "compose.yml", Platform: "linux/amd64"}
	manifest, err := BuildCompose(context.Background(), ComposeBuildInput{Checkout: Checkout{Root: root, Document: doc}, Version: "1.0.0", Registry: RegistryAuth{Host: "registry.example", Username: "push", Password: "registry-password"}})
	if err != nil || !strings.Contains(manifest, "@sha256:") || strings.Contains(manifest, `"build"`) {
		t.Fatalf("compose build: %s %v", manifest, err)
	}
}

func TestNamedEnvironmentRejectsUnscopedComposeBuildTags(t *testing.T) {
	tag := "ideploy-env-deployment-1.2.3"
	file := `{"services":{"api":{"image":"registry.invalid/api:` + tag + `","build":{"tags":["registry.invalid/api:latest"]}}}}`
	if err := ValidateComposeArtifactTags(file, tag); err == nil {
		t.Fatal("accepted an unscoped additional build tag")
	}
	file = strings.Replace(file, "api:latest", "api:"+tag, 1)
	if err := ValidateComposeArtifactTags(file, tag); err != nil {
		t.Fatal(err)
	}
}
