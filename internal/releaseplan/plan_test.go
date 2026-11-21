package releaseplan

import (
	"bytes"
	"testing"
)

func TestDocumentIsDeterministicAndRejectsEscapingPaths(t *testing.T) {
	d := Document{SchemaVersion: 1, DeploymentID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ReleaseID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SourceCommit: "cccccccccccccccccccccccccccccccccccccccc", WorkloadKind: "container", ContextPath: ".", Dockerfile: "Dockerfile", ImagePath: "registry.example/team/app", Platform: "linux/arm64"}
	name, first, digest, err := d.Encode()
	if err != nil || name != ".ideploy/releases/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.json" || digest == "" {
		t.Fatalf("encode: %s %v", name, err)
	}
	_, second, secondDigest, err := d.Encode()
	if err != nil || !bytes.Equal(first, second) || digest != secondDigest {
		t.Fatal("release document changed between encodes")
	}
	d.Dockerfile = "../secret"
	if _, _, _, err := d.Encode(); err == nil {
		t.Fatal("path escaped source directory")
	}
}
