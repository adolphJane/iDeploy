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

func TestFilterInspectableBaseImagesDeduplicatesStages(t *testing.T) {
	images := []string{"harbor.example.com/proxy/node:22-alpine", "harbor.example.com/proxy/node:22-alpine", "scratch", "${BASE_IMAGE}", "alpine:3.21"}
	want := []string{"harbor.example.com/proxy/node:22-alpine", "alpine:3.21"}
	if got := filterInspectableBaseImages(images); !reflect.DeepEqual(got, want) {
		t.Fatalf("inspectable images = %#v, want %#v", got, want)
	}
}

func TestPreflightProgressTracksCurrentStageAndCopiesChecks(t *testing.T) {
	service := NewSystemService()
	checks := []DeploymentPreflightCheckView{{ID: "source", Status: "ok", Detail: "源码可用"}}
	service.setPreflightProgress("request-1", 3, "正在检查基础镜像（0/1）", checks, false)
	checks[0].Detail = "已修改"
	progress, err := service.GetPreflightProgress("request-1")
	if err != nil || progress.Stage != 3 || progress.Total != 5 || progress.Completed || progress.Checks[0].Detail != "源码可用" {
		t.Fatalf("unexpected progress: %#v, %v", progress, err)
	}
	service.setPreflightProgress("request-1", 5, "检查已结束", progress.Checks, true)
	progress, err = service.GetPreflightProgress("request-1")
	if err != nil || !progress.Completed || progress.Stage != 5 {
		t.Fatalf("unexpected completed progress: %#v, %v", progress, err)
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
