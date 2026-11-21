package buildexecutor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ideploy/internal/releaseplan"
)

func TestBuildContainerPassesSecretsAsFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = login ]; then
  read password
  [ "$password" = registry-password ] || exit 2
  exit 0
fi
[ "$1" = buildx ] && [ "$2" = build ] || exit 3
while [ "$#" -gt 0 ]; do
  case "$1" in
    --metadata-file) metadata="$2"; shift 2 ;;
    --secret) secret="$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "$secret" in id=PACKAGE_TOKEN,src=*) ;; *) exit 4 ;; esac
file="${secret#*,src=}"
[ "$(cat "$file")" = build-secret ] || exit 5
printf '{"containerimage.digest":"sha256:%s"}' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' > "$metadata"
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	doc := releaseplan.Document{SchemaVersion: 1, DeploymentID: strings.Repeat("a", 32), ReleaseID: strings.Repeat("b", 32), SourceCommit: strings.Repeat("c", 40), WorkloadKind: "container", ContextPath: ".", Dockerfile: "Dockerfile", ImagePath: "registry.example/team/app", Platform: "linux/amd64"}
	image, err := BuildContainer(context.Background(), ContainerBuildInput{Checkout: Checkout{Root: root, Document: doc}, Version: "v1", Registry: RegistryAuth{Host: "registry.example", Username: "push", Password: "registry-password"}, Secrets: map[string]string{"PACKAGE_TOKEN": "build-secret"}})
	if err != nil || image != "registry.example/team/app@sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("build: %q %v", image, err)
	}
}

func TestBuildRejectsImageOutsideSelectedRegistry(t *testing.T) {
	for _, item := range []struct {
		image, host string
		valid       bool
	}{
		{"registry.example/team/app", "registry.example", true},
		{"other.example/team/app", "registry.example", false},
		{"team/app", "registry.example", false},
		{"team/app", "docker.io", true},
		{"registry.example/team/../app", "registry.example", false},
	} {
		if got := imageRepositoryMatchesRegistry(item.image, item.host); got != item.valid {
			t.Fatalf("image %q host %q: got %v", item.image, item.host, got)
		}
	}
}
