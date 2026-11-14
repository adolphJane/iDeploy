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
