package server

import "testing"

func TestParseListeningPortsMapsPublishedAndHostNetworkServices(t *testing.T) {
	sockets := `LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=1,fd=6))
LISTEN 0 511 0.0.0.0:8080 0.0.0.0:* users:(("docker-proxy",pid=2,fd=8))
LISTEN 0 511 [::]:8080 [::]:* users:(("docker-proxy",pid=3,fd=8))
LISTEN 0 511 127.0.0.1:1514 0.0.0.0:* users:(("docker-proxy",pid=4,fd=8))`
	docker := `harbor-nginx|0.0.0.0:8080->8080/tcp, [::]:8080->8080/tcp|harbor|nginx`
	items := parseListeningPorts(sockets, docker, "dev-nginx|nginx:alpine")
	if len(items) != 2 || items[0].Port != 80 || items[0].Service != "dev-nginx" || items[1].Port != 8080 || items[1].Service != "harbor / nginx" || len(items[1].BindAddresses) != 2 {
		t.Fatalf("unexpected ports: %+v", items)
	}
}
