package server

import (
	"strings"
	"testing"
)

func TestNginxManagedPathRoutes(t *testing.T) {
	routes := []IngressRoute{
		{FQDN: "apps.example.com", PathPrefix: "/demo/", HostIP: "127.0.0.1", HostPort: 8101},
		{FQDN: "apps.example.com", PathPrefix: "/docs/", HostIP: "127.0.0.1", HostPort: 8102},
	}
	block, err := NginxManagedBlock(routes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(block, "server_name apps.example.com;") != 1 || !strings.Contains(block, "location ^~ /demo/") || !strings.Contains(block, "proxy_pass http://127.0.0.1:8101;") || strings.Contains(block, "proxy_pass http://127.0.0.1:8101/;") {
		t.Fatalf("unexpected path routes: %s", block)
	}
	if !strings.Contains(block, "X-iDeploy-Path "+IngressRouteMarker("apps.example.com/demo/")) {
		t.Fatal("path location is missing its verification marker")
	}
	if _, err := NginxManagedBlock(append(routes, routes[0])); err == nil {
		t.Fatal("duplicate path accepted")
	}
	withGateway, err := NginxManagedBlock(append(routes, IngressRoute{FQDN: "apps.example.com", HostIP: "127.0.0.1", HostPort: 8103}))
	if err != nil || !strings.Contains(withGateway, "location / {") || !strings.Contains(withGateway, "proxy_pass http://127.0.0.1:8103;") {
		t.Fatalf("gateway fallback missing: %v %s", err, withGateway)
	}
	bad := routes[0]
	bad.PathPrefix = "/demo;return 200/"
	if _, err := NginxManagedBlock([]IngressRoute{bad}); err == nil {
		t.Fatal("unsafe path accepted")
	}
	tls := routes[0]
	tls.Certificate = "/etc/letsencrypt/live/apps.example.com/fullchain.pem"
	tls.CertificateKey = "/etc/letsencrypt/live/apps.example.com/privkey.pem"
	secured, err := NginxManagedBlock([]IngressRoute{tls})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(secured, "add_header X-iDeploy-Route "+IngressRouteMarker(tls.FQDN)) != 3 {
		t.Fatalf("HTTP and HTTPS must both expose route marker: %s", secured)
	}
	secureGateway := IngressRoute{FQDN: tls.FQDN, HostIP: "127.0.0.1", HostPort: 8766,
		Certificate: tls.Certificate, CertificateKey: tls.CertificateKey}
	secured, err = NginxManagedBlock([]IngressRoute{tls, secureGateway})
	if err != nil || strings.Count(secured, "server_name apps.example.com;") != 2 ||
		!strings.Contains(secured, "location / {\n            proxy_set_header Host $host;") ||
		!strings.Contains(secured, "proxy_pass http://127.0.0.1:8766;") {
		t.Fatalf("HTTPS project path and gateway root not combined: %v %s", err, secured)
	}
}

func TestNginxPathRoutesPreserveExistingConfig(t *testing.T) {
	base := "events {}\nhttp {\n    server { listen 80; server_name existing.example.com; }\n}\n"
	routes := []IngressRoute{{FQDN: "apps.example.com", PathPrefix: "/demo/", HostIP: "127.0.0.1", HostPort: 8101}, {FQDN: "apps.example.com", PathPrefix: "/docs/", HostIP: "127.0.0.1", HostPort: 8102}}
	first, _, err := NginxConfigWithRoutes(base, routes)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := NginxConfigWithRoutes(first, routes)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.Contains(first, "server_name existing.example.com") {
		t.Fatal("path config is not idempotent or changed existing server")
	}
}
