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

func TestReleaseDownloadURL(t *testing.T) {
	const base = "https://github.com/adolphJane/iDeploy/releases/download/v0.5.3/"
	release := githubRelease{
		HTMLURL: "https://github.com/adolphJane/iDeploy/releases/tag/v0.5.3",
		Assets: []githubReleaseAsset{
			{Name: "iDeploy-v0.5.3-macos-universal.zip", BrowserDownloadURL: base + "iDeploy-v0.5.3-macos-universal.zip"},
			{Name: "iDeploy-v0.5.3-macos-universal.dmg", BrowserDownloadURL: base + "iDeploy-v0.5.3-macos-universal.dmg"},
		},
	}
	if got := releaseDownloadURL(release); got != base+"iDeploy-v0.5.3-macos-universal.dmg" {
		t.Fatalf("preferred download = %q", got)
	}
	release.Assets = release.Assets[:1]
	if got := releaseDownloadURL(release); got != base+"iDeploy-v0.5.3-macos-universal.zip" {
		t.Fatalf("zip fallback = %q", got)
	}
	release.Assets = nil
	if got := releaseDownloadURL(release); got != release.HTMLURL {
		t.Fatalf("release page fallback = %q", got)
	}
}

func TestAppUpdateAssetName(t *testing.T) {
	const valid = "https://github.com/adolphJane/iDeploy/releases/download/v0.5.3/iDeploy-v0.5.3-macos-universal.dmg"
	if name, err := appUpdateAssetName(valid); err != nil || name != "iDeploy-v0.5.3-macos-universal.dmg" {
		t.Fatalf("valid release asset: name=%q, error=%v", name, err)
	}
	for _, rawURL := range []string{
		"http://github.com/adolphJane/iDeploy/releases/download/v0.5.3/iDeploy-v0.5.3.dmg",
		"https://github.com.evil.test/adolphJane/iDeploy/releases/download/v0.5.3/iDeploy-v0.5.3.dmg",
		"https://github.com:444/adolphJane/iDeploy/releases/download/v0.5.3/iDeploy-v0.5.3.dmg",
		"https://github.com/another/repo/releases/download/v0.5.3/iDeploy-v0.5.3.dmg",
		"https://github.com/adolphJane/iDeploy/releases/download/v0.5.3/other.dmg",
	} {
		if _, err := appUpdateAssetName(rawURL); err == nil {
			t.Errorf("accepted invalid release asset %q", rawURL)
		}
	}
}
