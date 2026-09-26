package main

import "testing"

func TestReleasePlatform(t *testing.T) {
	for input, want := range map[string]string{"linux/amd64/v4": "linux/amd64", "linux/x86_64": "linux/amd64", "linux/arm64": "linux/arm64", "linux/aarch64\n": "linux/arm64"} {
		got, err := releasePlatform(input)
		if err != nil || got != want {
			t.Fatalf("%q: got %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "windows/amd64", "linux/unknown"} {
		if _, err := releasePlatform(input); err == nil {
			t.Fatalf("accepted unknown platform %q", input)
		}
	}
}

func TestComposeRejectsWrongPlatform(t *testing.T) {
	if _, err := composeBuildImagesForPlatform(`{"services":{"api":{"image":"host/team/api:1","build":{},"platform":"linux/arm64"}}}`, "linux/amd64"); err == nil {
		t.Fatal("accepted ARM service on AMD64")
	}
	images, err := composeBuildImagesForPlatform(`{"services":{"api":{"image":"host/team/api:1","build":{}},"db":{"image":"postgres:17"}}}`, "linux/amd64")
	if err != nil || len(images) != 1 || images[0] != "host/team/api:1" {
		t.Fatalf("unexpected build images: %v, %v", images, err)
	}
	if _, err := composeBuildImagesForPlatform(`{"services":{"api":{"build":{}}}}`, "linux/amd64"); err == nil {
		t.Fatal("accepted unpublishable service")
	}
}
