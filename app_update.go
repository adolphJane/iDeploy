package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const latestReleaseAPIURL = "https://api.github.com/repos/adolphJane/iDeploy/releases/latest"

type AppUpdateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseURL"`
	PublishedAt     string `json:"publishedAt"`
	CheckedAt       string `json:"checkedAt"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

var appVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)

func (s *SystemService) GetAppUpdateStatus() (AppUpdateStatus, error) {
	currentVersion := strings.TrimPrefix(appVersion, "v")
	if _, ok := parseAppVersion(currentVersion); !ok {
		return AppUpdateStatus{}, fmt.Errorf("无法识别当前应用版本 %q", appVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPIURL, nil)
	if err != nil {
		return AppUpdateStatus{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "iDeploy/"+currentVersion)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return AppUpdateStatus{}, fmt.Errorf("连接 GitHub 检查更新失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return AppUpdateStatus{}, fmt.Errorf("GitHub 更新服务返回 HTTP %d", response.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return AppUpdateStatus{}, fmt.Errorf("读取 GitHub 版本信息失败: %w", err)
	}
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if _, ok := parseAppVersion(latestVersion); !ok {
		return AppUpdateStatus{}, errors.New("GitHub 最新 Release 的版本标签格式无效")
	}
	comparison, ok := compareAppVersions(latestVersion, currentVersion)
	if !ok {
		return AppUpdateStatus{}, errors.New("无法比较应用版本")
	}

	return AppUpdateStatus{
		CurrentVersion:  currentVersion,
		LatestVersion:   latestVersion,
		UpdateAvailable: comparison > 0,
		ReleaseURL:      release.HTMLURL,
		PublishedAt:     release.PublishedAt,
		CheckedAt:       time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func parseAppVersion(version string) ([3]int, bool) {
	match := appVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return [3]int{}, false
	}
	var parts [3]int
	for i := range parts {
		value, err := strconv.Atoi(match[i+1])
		if err != nil {
			return [3]int{}, false
		}
		parts[i] = value
	}
	return parts, true
}

func compareAppVersions(left, right string) (int, bool) {
	leftParts, leftOK := parseAppVersion(left)
	rightParts, rightOK := parseAppVersion(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	for i := range leftParts {
		if leftParts[i] > rightParts[i] {
			return 1, true
		}
		if leftParts[i] < rightParts[i] {
			return -1, true
		}
	}
	leftPrerelease := strings.Contains(left, "-")
	rightPrerelease := strings.Contains(right, "-")
	if leftPrerelease && !rightPrerelease {
		return -1, true
	}
	if !leftPrerelease && rightPrerelease {
		return 1, true
	}
	return 0, true
}
