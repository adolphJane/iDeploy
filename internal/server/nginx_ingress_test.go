package server

import (
	"strings"
	"testing"
)

func TestNginxManagedRoutesPreserveExistingConfig(t *testing.T) {
	base := `events {}
http {
    server { listen 80; server_name existing.example.com; }
}
`
	route := IngressRoute{FQDN: "ifx.example.com", HostIP: "0.0.0.0", HostPort: 8101}
	first, block, err := NginxConfigWithRoutes(base, []IngressRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "server_name existing.example.com") || !strings.Contains(block, "proxy_pass http://127.0.0.1:8101;") || !strings.Contains(block, IngressRouteMarker(route.FQDN)) {
		t.Fatalf("unexpected Nginx route: %s", first)
	}
	second, _, err := NginxConfigWithRoutes(first, []IngressRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("reapply changed config:\n%s\n---\n%s", first, second)
	}
	removed, _, err := NginxConfigWithRoutes(first, nil)
	if err != nil || removed != base {
		t.Fatalf("remove should restore original config: %v\n%s", err, removed)
	}
	if _, _, err := NginxConfigWithRoutes(base, []IngressRoute{{FQDN: "existing.example.com", HostIP: "127.0.0.1", HostPort: 8000}}); err == nil {
		t.Fatal("existing hostname was overwritten")
	}
	if !nginxDeclaresName("server_name first.example.com ifx.example.com;", "ifx.example.com") {
		t.Fatal("secondary server_name was missed")
	}
}

func TestNginxManagedRoutesRejectInvalidOrAmbiguousConfig(t *testing.T) {
	route := []IngressRoute{{FQDN: "api.example.com", HostIP: "127.0.0.1", HostPort: 3000}}
	for _, base := range []string{
		"http {\n    # BEGIN iDeploy managed Nginx routes\n}\n",
		"http {\n}\nhttp {\n}\n",
		"events {\n}\n",
		"http { server { listen 80; }",
	} {
		if _, _, err := NginxConfigWithRoutes(base, route); err == nil {
			t.Fatalf("accepted ambiguous config: %q", base)
		}
	}
	if _, _, err := NginxConfigWithRoutes("http {}", []IngressRoute{{FQDN: "bad;example.com", HostIP: "127.0.0.1", HostPort: 3000}}); err == nil {
		t.Fatal("accepted unsafe hostname")
	}
}

func TestParseNginxRoutes(t *testing.T) {
	config := `http {
 server { listen 80; server_name harbor.example.com; return 301 https://$host$request_uri; }
 server {
  listen 443 ssl;
  server_name harbor.example.com;
  ssl_certificate /etc/letsencrypt/live/harbor/fullchain.pem;
  ssl_certificate_key /etc/letsencrypt/live/harbor/privkey.pem;
  location / { proxy_pass https://127.0.0.1:8443; }
 }
}`
	routes := parseNginxRoutes(config)
	if len(routes) != 1 || routes[0].FQDN != "harbor.example.com" || !routes[0].HTTP || !routes[0].HTTPS || routes[0].Upstream != "https://127.0.0.1:8443" || routes[0].Certificate == "" {
		t.Fatalf("wrong route scan: %+v", routes)
	}
}

func TestNginxHTTPSRouteRequiresCertificatePair(t *testing.T) {
	route := IngressRoute{FQDN: "harbor.example.com", HostIP: "127.0.0.1", HostPort: 8080,
		Certificate:    "/etc/letsencrypt/live/harbor.example.com/fullchain.pem",
		CertificateKey: "/etc/letsencrypt/live/harbor.example.com/privkey.pem"}
	block, err := NginxManagedBlock([]IngressRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"listen 80;", "return 301 https://$host$request_uri;", "listen 443 ssl;", "proxy_pass http://127.0.0.1:8080;", route.Certificate, route.CertificateKey} {
		if !strings.Contains(block, part) {
			t.Fatalf("missing %q in %s", part, block)
		}
	}
	route.CertificateKey = ""
	if _, err := NginxManagedBlock([]IngressRoute{route}); err == nil {
		t.Fatal("accepted incomplete HTTPS certificate pair")
	}
}

func TestNginxHTTPSRoutePreservesACMEChallenge(t *testing.T) {
	base := `events {}
http {
    server {
        listen 80 default_server;
        location ^~ /.well-known/acme-challenge/ {
            root /var/www/ideploy-acme;
        }
    }
}
`
	route := IngressRoute{
		FQDN: "ifx.example.com", HostIP: "127.0.0.1", HostPort: 8101,
		Certificate:    "/etc/letsencrypt/live/ifx.example.com/fullchain.pem",
		CertificateKey: "/etc/letsencrypt/live/ifx.example.com/privkey.pem",
	}
	first, block, err := NginxConfigWithRoutes(base, []IngressRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(block, "root /var/www/ideploy-acme;") != 1 || !strings.Contains(block, "location / { return 301 https://$host$request_uri; }") {
		t.Fatalf("managed HTTP route must serve ACME before redirect: %s", block)
	}
	second, _, err := NginxConfigWithRoutes(first, []IngressRoute{route})
	if err != nil || first != second {
		t.Fatalf("reapply must preserve ACME route: %v", err)
	}
}

func TestParseDockerPublishedServices(t *testing.T) {
	output := "nginx|0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp, 127.0.0.1:8443->8443/tcp|harbor|nginx\n" +
		"api|192.0.2.2:3000->3000/tcp|project|api"
	items := parseDockerPublishedServices(output)
	if len(items) != 3 {
		t.Fatalf("expected 3 unique endpoints, got %+v", items)
	}
	if items[0].Service != "harbor / nginx" || items[0].HostIP != "127.0.0.1" || items[0].HostPort != 8080 {
		t.Fatalf("wrong harbor endpoint: %+v", items[0])
	}
	if items[1].HostPort != 8443 || items[2].HostIP != "192.0.2.2" {
		t.Fatalf("missing published ports: %+v", items)
	}
}
