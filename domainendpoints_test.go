package main

import "testing"

func TestParseDomainEndpoints(t *testing.T) {
	container := RemoteContainerView{Name: "api", Ports: "0.0.0.0:18080->3000/tcp, [::]:18080->3000/tcp, :::18081->3001/tcp, 53->53/udp, 3002/tcp"}
	got := parseDomainEndpoints(container)
	if len(got) != 2 {
		t.Fatalf("expected two published TCP endpoints, got %+v", got)
	}
	if got[0].HostIP != "0.0.0.0" || got[0].HostPort != 18080 || got[0].ContainerPort != 3000 || got[0].Status != "unverified" {
		t.Fatalf("unexpected endpoint: %+v", got[0])
	}
	if len(got[0].BindAddresses) != 2 || got[0].BindAddresses[0] != "0.0.0.0" || got[0].BindAddresses[1] != "::" {
		t.Fatalf("expected merged IPv4 and IPv6 listeners: %+v", got[0])
	}
	if got[1].HostIP != "::" || got[1].HostPort != 18081 {
		t.Fatalf("unexpected IPv6 endpoint: %+v", got[1])
	}
}

func TestParseDomainEndpointsKeepsDistinctAddresses(t *testing.T) {
	container := RemoteContainerView{Name: "api", Ports: "[::]:8101->8080/tcp, 0.0.0.0:8101->8080/tcp, 10.0.0.2:8101->8080/tcp"}
	got := parseDomainEndpoints(container)
	if len(got) != 2 || got[0].HostIP != "0.0.0.0" || len(got[0].BindAddresses) != 2 || got[1].HostIP != "10.0.0.2" {
		t.Fatalf("expected wildcard listeners merged and specific address preserved: %+v", got)
	}
}
