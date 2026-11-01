package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainerOperationJournal(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	count := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + count + "\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	store, err := NewOperationStore(filepath.Join(dir, "operations"))
	if err != nil {
		t.Fatal(err)
	}
	req := OperationRequest{ID: strings.Repeat("a", 32), Kind: "container.restart", ContainerID: "container-1"}
	if _, err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		op, err := store.Get(req.ID)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation did not finish: %+v", op)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	callData, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(callData) != "restart --time 10 container-1\n" {
		t.Fatalf("operation replayed or arguments changed: %q", callData)
	}
	req.Kind = "container.stop"
	if _, err := store.Create(req); err == nil {
		t.Fatal("accepted reused operation ID with different request")
	}
	if _, err := store.Create(OperationRequest{ID: strings.Repeat("b", 32), Kind: "shell.exec", ContainerID: "container-1"}); err == nil {
		t.Fatal("accepted arbitrary operation")
	}
}

func TestImageTagJournalVerifiesDigest(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = image ] && [ \"$2\" = inspect ]; then echo sha256:abc; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	store, err := NewOperationStore(filepath.Join(dir, "operations"))
	if err != nil {
		t.Fatal(err)
	}
	req := OperationRequest{ID: strings.Repeat("c", 32), Kind: "image.tag", Image: "registry.example/app@sha256:abc", TargetImage: "registry.example/app:v1"}
	if _, err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		op, err := store.Get(req.ID)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status == "succeeded" {
			break
		}
		if op.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("image tag failed: %+v", op)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDeployContainerRestoresPreviousOnRunFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "docker-calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\ncase \"$1 $2\" in\n  'container inspect') exit 0;;\nesac\nif [ \"$1\" = run ]; then exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	req := OperationRequest{ID: strings.Repeat("d", 32), Kind: "container.deploy", ContainerID: "api", Image: "registry.example/api:v1", RestartPolicy: "unless-stopped", Ports: []string{"8101:8080"}, Labels: map[string]string{"com.ideploy.version": "v1"}}
	if _, err := deployContainer(t.Context(), req); err == nil {
		t.Fatal("expected docker run failure")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	sequence := string(data)
	for _, part := range []string{"container inspect api", "rename api api.ideploy-", "stop --time 10 api.ideploy-", "run -d --restart unless-stopped --name api", "rm -f api", "rename api.ideploy-"} {
		if !strings.Contains(sequence, part) {
			t.Fatalf("missing recovery step %q in %s", part, sequence)
		}
	}
	if !strings.Contains(sequence, "start api") {
		t.Fatalf("previous container was not restarted: %s", sequence)
	}
}

func TestComposeManifestNotWrittenToOperationJournal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	store, err := NewOperationStore(filepath.Join(dir, "operations"))
	if err != nil {
		t.Fatal(err)
	}
	req := OperationRequest{ID: strings.Repeat("e", 32), Kind: "compose.upload", DeploymentID: "project1", Manifest: "services:\n  app:\n    image: demo\n    environment:\n      TOKEN: secret-sentinel-value\n"}
	if _, err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		op, err := store.Get(req.ID)
		if err != nil {
			t.Fatal(err)
		}
		if op.Status == "succeeded" {
			break
		}
		if op.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("compose upload failed: %+v", op)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(store.path(req.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-sentinel-value") {
		t.Fatal("manifest secret persisted in operation journal")
	}
	manifest, err := os.ReadFile(filepath.Join(dir, ".ideploy", "deployments", "project1", "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "secret-sentinel-value") {
		t.Fatal("compose manifest was not installed")
	}
}
