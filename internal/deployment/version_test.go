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
