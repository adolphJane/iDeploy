package deployment_test

import (
	"ideploy/internal/deployment"
	"ideploy/internal/server"
	"testing"
)

func TestMatchUsesWorkloadIdentityNotSharedImage(t *testing.T) {
	containers := []server.Container{
		{ID: "a", Name: "api", Image: "shared:latest", ComposeProject: "production"},
		{ID: "b", Name: "api-test", Image: "shared:latest", ComposeProject: "staging"},
		{ID: "c", Name: "production-worker", Image: "worker:latest", ComposeProject: "production"},
		{ID: "d", Name: "production-run", ComposeProject: "production", OneOff: true},
	}
	cases := []struct {
		name, kind, workload string
		count                int
	}{
		{"legacy binding cannot act", "", "", 0},
		{"exact container", "container", "api", 1},
		{"not a prefix", "container", "ap", 0},
		{"compose includes services but excludes oneoff", "compose", "production", 2},
		{"no image fallback", "container", "shared:latest", 0},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := deployment.Match(deployment.Target{WorkloadKind: tt.kind, WorkloadName: tt.workload}, containers)
			if len(got) != tt.count {
				t.Fatalf("got %#v, want %d", got, tt.count)
			}
		})
	}
}
