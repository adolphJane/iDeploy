package agent

import "testing"

func TestSuggestedACMEWebroot(t *testing.T) {
	config := `server { location ^~ /.well-known/acme-challenge/ { root /var/www/ideploy-acme; } }`
	mounts := []dockerMount{{Type: "bind", Source: "/data/nginx/acme", Destination: "/var/www/ideploy-acme"}}
	if got := suggestedACMEWebroot(config, mounts); got != "/data/nginx/acme" {
		t.Fatalf("suggested webroot = %q", got)
	}
	if got := suggestedACMEWebroot(config, nil); got != "" {
		t.Fatalf("unmounted webroot should not be suggested: %q", got)
	}
}
