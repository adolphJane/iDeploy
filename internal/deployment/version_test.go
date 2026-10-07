package deployment

import "testing"

func TestVersionPrecedenceAndSuggestion(t *testing.T) {
	for _, pair := range [][2]string{{"1.9.0", "1.10.0"}, {"1.0.0-rc.2", "1.0.0-rc.10"}, {"1.0.0-rc.10", "1.0.0"}} {
		if c, ok := CompareVersion(pair[0], pair[1]); !ok || c >= 0 {
			t.Fatalf("bad precedence: %v", pair)
		}
	}
	history := []Release{{Version: "1.0.0", Status: ReleaseSucceeded, Action: "rollback"}, {Version: "1.0.2", Status: ReleaseFailed}, {Version: "1.0.1", Status: ReleaseSucceeded}}
	if got := SuggestVersion("1.0.0", history); got != "1.0.3" {
		t.Fatalf("rollback/failed versions not accounted for: %s", got)
	}
	if got := SuggestVersion("2.0.0", history); got != "2.0.0" {
		t.Fatalf("higher source version ignored: %s", got)
	}
	for _, v := range []string{"v1.0.1", "1.0.2", "0.9.0", "latest", "1.0.0-01"} {
		if err := ValidateNewVersion(v, history); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	if got := SuggestVersion("", nil); got != "0.1.0" {
		t.Fatal(got)
	}
	if got := SuggestVersion("1.0.0-rc.1", []Release{{Version: "1.0.0-rc.1"}}); got != "1.0.0" {
		t.Fatal(got)
	}
}

func TestRetryVersionOnlyAllowsUnpublishedFailedAttempts(t *testing.T) {
	failed := []Release{{Version: "1.0.1", Action: "deploy", Status: ReleaseFailed}}
	if err := ValidateRetryVersion("1.0.1", failed); err != nil {
		t.Fatalf("safe retry rejected: %v", err)
	}
	for _, history := range [][]Release{
		{{Version: "1.0.1", Action: "deploy", Status: ReleaseSucceeded}},
		{{Version: "1.0.1", Action: "deploy", Status: ReleaseFailed, ImageRef: "registry.example/api@sha256:abc"}},
		{{Version: "1.0.1", Action: "deploy", Status: ReleaseQueued}},
	} {
		if err := ValidateRetryVersion("1.0.1", history); err == nil {
			t.Fatalf("unsafe retry accepted: %#v", history)
		}
	}
}

func TestResumeVersionOnlyAllowsFailedAttemptsWithArtifact(t *testing.T) {
	history := []Release{{Version: "1.0.1", Action: "deploy", Status: ReleaseFailed, ImageRef: "registry.example/api@sha256:abc"}}
	if err := ValidateResumeVersion("1.0.1", history); err != nil {
		t.Fatalf("safe resume rejected: %v", err)
	}
	for _, unsafe := range [][]Release{
		{{Version: "1.0.1", Action: "deploy", Status: ReleaseFailed}},
		{{Version: "1.0.1", Action: "deploy", Status: ReleaseSucceeded, ImageRef: "registry.example/api@sha256:abc"}},
		{{Version: "1.0.1", Action: "deploy", Status: ReleasePulling, ImageRef: "registry.example/api@sha256:abc"}},
		{{Version: "1.0.1", Action: "rollback", Status: ReleaseFailed, ImageRef: "registry.example/api@sha256:abc"}},
	} {
		if err := ValidateResumeVersion("1.0.1", unsafe); err == nil {
			t.Fatalf("unsafe resume accepted: %#v", unsafe)
		}
	}
}
