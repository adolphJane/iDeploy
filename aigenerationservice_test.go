package main

import (
	"strings"
	"testing"
)

func TestGeneratedDockerfileWarnsAboutUnverifiedProxyBaseImage(t *testing.T) {
	warnings := validateGeneratedDockerfile("FROM harbor.example.com/proxy/node:22-alpine\n")
	if !strings.Contains(strings.Join(warnings, "\n"), "Proxy Cache") {
		t.Fatalf("expected proxy cache warning, got %#v", warnings)
	}
}

func TestDockerfileBaseImagesSupportsPlatformAndStages(t *testing.T) {
	images := dockerfileBaseImages("FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build\nFROM scratch\nFROM alpine:3.22\n")
	if len(images) != 2 || images[0] != "golang:1.25-alpine" || images[1] != "alpine:3.22" {
		t.Fatalf("unexpected base images: %#v", images)
	}
}
