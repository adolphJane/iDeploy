package server

import (
	"archive/tar"
	"io"
	"strings"
	"testing"
)

func TestSharedServerBuildContextContainsOnlyProgramAndDockerfile(t *testing.T) {
	tag, archive, err := sharedServerBuildContext([]byte("binary-content"))
	if err != nil || !strings.HasPrefix(tag, "ideploy/shared-server:") {
		t.Fatalf("build context: %v", err)
	}
	reader := tar.NewReader(strings.NewReader(archive))
	names := []string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		if _, err = io.Copy(io.Discard, reader); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(names, ",") != "Dockerfile,ideploy-server" {
		t.Fatalf("unexpected build files: %v", names)
	}
}

func TestWorkerBuildContextContainsDockerToolsAndExecutable(t *testing.T) {
	tag, archive, err := workerBuildContext([]byte("worker-binary"))
	if err != nil || !strings.HasPrefix(tag, "ideploy/build-worker:") {
		t.Fatalf("build context: %v", err)
	}
	reader := tar.NewReader(strings.NewReader(archive))
	files := map[string]string{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = string(body)
		if header.Name == "ideploy-worker" && header.Mode&0111 == 0 {
			t.Fatal("worker binary is not executable")
		}
	}
	if files["ideploy-worker"] != "worker-binary" || !strings.Contains(files["Dockerfile"], "git openssh-client") || !strings.Contains(files["Dockerfile"], "docker:27-cli") {
		t.Fatalf("invalid worker image contents: %v", files)
	}
}
