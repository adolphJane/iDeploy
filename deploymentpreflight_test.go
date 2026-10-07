package main

import (
	"reflect"
	"testing"
)

func TestMirroredBaseImageReference(t *testing.T) {
	rules := []BuildProxyRule{{Upstream: "docker.io", Mirror: "harbor.example.com/proxy"}, {Upstream: "ghcr.io", Mirror: "harbor.example.com/proxy-ghcr"}}
	for input, want := range map[string]string{
		"node:22-alpine":      "harbor.example.com/proxy/library/node:22-alpine",
		"team/api:v1":         "harbor.example.com/proxy/team/api:v1",
		"ghcr.io/team/api:v1": "harbor.example.com/proxy-ghcr/team/api:v1",
		"quay.io/team/api:v1": "quay.io/team/api:v1",
	} {
		if got := mirroredBaseImageReference(input, rules); got != want {
			t.Fatalf("mirror %q = %q, want %q", input, got, want)
		}
	}
}

func TestPreflightPortParsingAndSuggestions(t *testing.T) {
	if got, want := requestedHostPorts([]string{"8080:3000", "127.0.0.1:8443:443/tcp"}), []int{8080, 8443}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requested ports = %#v, want %#v", got, want)
	}
	if got := publishedHostPorts("0.0.0.0:8080->3000/tcp, [::]:8080->3000/tcp, 127.0.0.1:9000->90/tcp"); !reflect.DeepEqual(got, []int{8080, 9000}) {
		t.Fatalf("published ports = %#v", got)
	}
	if got := suggestAvailablePorts([]int{8080}, map[int]string{8081: "one", 8082: "two"}); !reflect.DeepEqual(got, []int{8083, 8084, 8085}) {
		t.Fatalf("suggested ports = %#v", got)
	}
}
