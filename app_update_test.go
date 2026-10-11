package main

import "testing"

func TestCompareAppVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
		ok    bool
	}{
		{name: "newer patch", left: "0.5.3", right: "0.5.2", want: 1, ok: true},
		{name: "numeric components", left: "0.10.0", right: "0.9.9", want: 1, ok: true},
		{name: "stable newer than prerelease", left: "1.0.0", right: "1.0.0-rc.1", want: 1, ok: true},
		{name: "older stable", left: "1.2.2", right: "1.2.3", want: -1, ok: true},
		{name: "same version", left: "1.2.3", right: "1.2.3", want: 0, ok: true},
		{name: "invalid version", left: "latest", right: "1.2.3", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := compareAppVersions(test.left, test.right)
			if got != test.want || ok != test.ok {
				t.Fatalf("compareAppVersions(%q, %q) = (%d, %v), want (%d, %v)", test.left, test.right, got, ok, test.want, test.ok)
			}
		})
	}
}
