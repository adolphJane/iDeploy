package server

import (
	"strings"
	"testing"
)

func TestManagedCaddyfile(t *testing.T) {
	config, err := ManagedCaddyfile([]IngressRoute{
		{FQDN: "www.example.com", HostIP: "::", HostPort: 18081},
		{FQDN: "example.com", HostIP: "0.0.0.0", HostPort: 18080},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config, "reverse_proxy 127.0.0.1:18080") ||
		!strings.Contains(config, "reverse_proxy [::1]:18081") ||
		!strings.Contains(config, "header X-iDeploy-Route "+IngressRouteMarker("example.com")) {
		t.Fatalf("unexpected config: %s", config)
	}
	if strings.Index(config, "example.com {") > strings.Index(config, "www.example.com {") {
		t.Fatal("routes not sorted")
	}
	if len(CaddyfileHash(config)) != 64 || CaddyfileHash(config) == CaddyfileHash(config+" ") {
		t.Fatal("config hash is invalid")
	}
	for _, route := range []IngressRoute{
		{FQDN: "bad.example.com\nrespond 200", HostIP: "127.0.0.1", HostPort: 80},
		{FQDN: "example.com", HostIP: "shell-command", HostPort: 80},
		{FQDN: "example.com", HostIP: "127.0.0.1", HostPort: 0},
	} {
		if _, err := ManagedCaddyfile([]IngressRoute{route}); err == nil {
			t.Errorf("expected rejection of %+v", route)
		}
	}
}

func TestIngressPortConflictDetail(t *testing.T) {
	output := `80/TCP|LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=100,fd=6))
443/TCP|LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:(("nginx",pid=100,fd=7))
443/TCP|LISTEN 0 511 [::]:443 [::]:* users:(("nginx",pid=100,fd=8))`
	detail := ingressPortConflictDetail(output)
	if !strings.Contains(detail, "80/TCP（nginx）") || !strings.Contains(detail, "443/TCP（nginx）") || strings.Count(detail, "443/TCP（nginx）") != 1 {
		t.Fatalf("unexpected conflict detail: %s", detail)
	}
	if ingressPortConflictDetail("") != "" {
		t.Fatal("empty socket listing should have no conflict")
	}
	if detail := ingressPortConflictDetail("443/UDP|UNCONN 0 0 0.0.0.0:443 0.0.0.0:*"); !strings.Contains(detail, "443/UDP") {
		t.Fatalf("UDP listener missing: %s", detail)
	}
}
