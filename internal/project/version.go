package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// VersionSuggestion is intentionally advisory. A project can still publish
// with any Docker-compatible tag; this value only pre-fills the release form.
type VersionSuggestion struct {
	Version string
	Source  string
}

var projectVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// DetectLocalVersion looks only at common, bounded project metadata files. It
// does not execute package managers or inspect generated dependency folders.
func DetectLocalVersion(root, contextPath string) (VersionSuggestion, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return VersionSuggestion{}, errors.New("local project path is empty")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return VersionSuggestion{}, err
	}
	base = filepath.Clean(base)
	if contextPath != "" && contextPath != "." {
		candidate := filepath.Clean(filepath.Join(base, filepath.FromSlash(contextPath)))
		if !isWithin(base, candidate) {
			return VersionSuggestion{}, errors.New("build context escapes project path")
		}
		base = candidate
	}

	for _, candidate := range []struct {
		name string
		read func([]byte) string
	}{
		{"package.json", packageJSONVersion},
		{"pubspec.yaml", yamlVersion},
		{"Cargo.toml", cargoVersion},
		{"pyproject.toml", pythonProjectVersion},
		{"VERSION", plainVersion},
		{"version", plainVersion},
	} {
		contents, readErr := os.ReadFile(filepath.Join(base, candidate.name))
		if readErr != nil {
			continue
		}
		if version := normalizeProjectVersion(candidate.read(contents)); version != "" {
			return VersionSuggestion{Version: version, Source: candidate.name}, nil
		}
	}
	return VersionSuggestion{}, nil
}

func packageJSONVersion(contents []byte) string {
	var value struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(contents, &value) != nil {
		return ""
	}
	return value.Version
}

var yamlVersionPattern = regexp.MustCompile(`(?m)^\s*version\s*:\s*["']?([^\s"'#]+)`)

func yamlVersion(contents []byte) string {
	matches := yamlVersionPattern.FindSubmatch(contents)
	if len(matches) == 2 {
		return string(matches[1])
	}
	return ""
}

func cargoVersion(contents []byte) string {
	section := ""
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section == "package" {
			if matches := regexp.MustCompile(`^version\s*=\s*["']([^"']+)["']`).FindStringSubmatch(line); len(matches) == 2 {
				return matches[1]
			}
		}
	}
	return ""
}

func pythonProjectVersion(contents []byte) string {
	section := ""
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section == "project" {
			if matches := regexp.MustCompile(`^version\s*=\s*["']([^"']+)["']`).FindStringSubmatch(line); len(matches) == 2 {
				return matches[1]
			}
		}
	}
	return ""
}

func plainVersion(contents []byte) string {
	return strings.TrimSpace(strings.SplitN(string(contents), "\n", 2)[0])
}

func normalizeProjectVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	if projectVersionPattern.MatchString(value) {
		return value
	}
	return ""
}

func isWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
