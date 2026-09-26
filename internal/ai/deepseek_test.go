package ai

import "testing"

func TestCleanDockerfile(t *testing.T) {
	value := cleanDockerfile("```dockerfile\nFROM alpine:3.20\n```")
	if value != "FROM alpine:3.20\n" {
		t.Fatalf("unexpected cleaned Dockerfile: %q", value)
	}
}
