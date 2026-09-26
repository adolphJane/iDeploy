package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const browsePageSize = 100

type Repository struct {
	Name          string
	ArtifactCount int
	PullCount     int64
	UpdatedAt     string
}

// Project is a Harbor project namespace. Standard OCI registries do not expose
// this concept, so callers should only request projects from Harbor connections.
type Project struct {
	Name            string
	RepositoryCount int
}

type Artifact struct {
	Digest    string
	Tags      []string
	Size      int64
	CreatedAt string
	MediaType string
}

type BrowseError struct {
	Status Status
	Detail string
}

func (e *BrowseError) Error() string {
	return e.Detail
}

type Browser struct {
	client *http.Client
}

func NewBrowser() *Browser {
	return &Browser{client: &http.Client{Timeout: 12 * time.Second}}
}

func (b *Browser) ListProjects(ctx context.Context, connection Connection) ([]Project, error) {
	if connection.Kind != KindHarbor {
		return nil, &BrowseError{Status: StatusDegraded, Detail: "标准 OCI Registry 不提供 Harbor 项目列表"}
	}
	return b.listHarborProjects(ctx, connection)
}

func (b *Browser) ListRepositories(ctx context.Context, connection Connection, project string) ([]Repository, error) {
	if connection.Kind == KindHarbor {
		project = strings.Trim(strings.TrimSpace(project), "/")
		if project == "" {
			return nil, &BrowseError{Status: StatusDegraded, Detail: "请选择 Harbor 项目后再浏览镜像仓库"}
		}
		return b.listHarborRepositories(ctx, connection, project)
	}
	return b.listOCIRepositories(ctx, connection)
}

func (b *Browser) ListArtifacts(ctx context.Context, connection Connection, repository string) ([]Artifact, error) {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		return nil, &BrowseError{Status: StatusDegraded, Detail: "镜像仓库名称不能为空"}
	}
	if connection.Kind == KindHarbor {
		return b.listHarborArtifacts(ctx, connection, repository)
	}
	return b.listOCIArtifacts(ctx, connection, repository)
}

func (b *Browser) listHarborProjects(ctx context.Context, connection Connection) ([]Project, error) {
	var projects []struct {
		Name      string `json:"name"`
		RepoCount int    `json:"repo_count"`
	}
	if err := b.getJSON(ctx, connection, connection.Endpoint+"/api/v2.0/projects?page=1&page_size=100", &projects); err != nil {
		return nil, err
	}
	result := make([]Project, 0, len(projects))
	for _, project := range projects {
		if project.Name != "" {
			result = append(result, Project{Name: project.Name, RepositoryCount: project.RepoCount})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (b *Browser) listHarborRepositories(ctx context.Context, connection Connection, project string) ([]Repository, error) {
	var response []struct {
		Name          string `json:"name"`
		ArtifactCount int    `json:"artifact_count"`
		PullCount     int64  `json:"pull_count"`
		UpdateTime    string `json:"update_time"`
	}
	endpoint := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories?page=1&page_size=%d",
		connection.Endpoint, url.PathEscape(project), browsePageSize)
	if err := b.getJSON(ctx, connection, endpoint, &response); err != nil {
		return nil, err
	}

	repositories := make([]Repository, 0, len(response))
	for _, item := range response {
		if item.Name == "" {
			continue
		}
		repositories = append(repositories, Repository{
			Name:          item.Name,
			ArtifactCount: item.ArtifactCount,
			PullCount:     item.PullCount,
			UpdatedAt:     item.UpdateTime,
		})
	}
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Name < repositories[j].Name })
	return repositories, nil
}

func (b *Browser) listHarborArtifacts(ctx context.Context, connection Connection, repository string) ([]Artifact, error) {
	project, repositoryName, found := strings.Cut(repository, "/")
	if !found || project == "" || repositoryName == "" {
		return nil, &BrowseError{Status: StatusDegraded, Detail: "Harbor 镜像仓库名称必须包含项目空间"}
	}
	var response []struct {
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
		PushTime  string `json:"push_time"`
		MediaType string `json:"media_type"`
		Tags      []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	endpoint := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts?with_tag=true&with_scan_overview=false&with_label=false&page=1&page_size=%d",
		connection.Endpoint, url.PathEscape(project), url.PathEscape(repositoryName), browsePageSize)
	if err := b.getJSON(ctx, connection, endpoint, &response); err != nil {
		return nil, err
	}

	artifacts := make([]Artifact, 0, len(response))
	for _, item := range response {
		tags := make([]string, 0, len(item.Tags))
		for _, tag := range item.Tags {
			if tag.Name != "" {
				tags = append(tags, tag.Name)
			}
		}
		sort.Strings(tags)
		artifacts = append(artifacts, Artifact{
			Digest:    item.Digest,
			Tags:      tags,
			Size:      item.Size,
			CreatedAt: item.PushTime,
			MediaType: item.MediaType,
		})
	}
	return artifacts, nil
}

func (b *Browser) listOCIRepositories(ctx context.Context, connection Connection) ([]Repository, error) {
	var response struct {
		Repositories []string `json:"repositories"`
	}
	if err := b.getJSON(ctx, connection, fmt.Sprintf("%s/v2/_catalog?n=%d", connection.Endpoint, browsePageSize), &response); err != nil {
		return nil, err
	}
	repositories := make([]Repository, 0, len(response.Repositories))
	for _, name := range response.Repositories {
		if name != "" {
			repositories = append(repositories, Repository{Name: name})
		}
	}
	sort.Slice(repositories, func(i, j int) bool { return repositories[i].Name < repositories[j].Name })
	return repositories, nil
}

func (b *Browser) listOCIArtifacts(ctx context.Context, connection Connection, repository string) ([]Artifact, error) {
	var response struct {
		Tags []string `json:"tags"`
	}
	endpoint := fmt.Sprintf("%s/v2/%s/tags/list?n=%d", connection.Endpoint, escapeRepository(repository), browsePageSize)
	if err := b.getJSON(ctx, connection, endpoint, &response); err != nil {
		return nil, err
	}
	sort.Strings(response.Tags)
	artifacts := make([]Artifact, 0, len(response.Tags))
	for _, tag := range response.Tags {
		artifacts = append(artifacts, Artifact{Tags: []string{tag}, MediaType: "OCI tag"})
	}
	return artifacts, nil
}

func (b *Browser) getJSON(ctx context.Context, connection Connection, endpoint string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &BrowseError{Status: StatusDegraded, Detail: fmt.Sprintf("无法创建仓库查询请求：%v", err)}
	}
	request.Header.Set("Accept", "application/json")
	if connection.Username != "" && connection.CredentialRef != "" {
		if password, err := LoadPassword(ctx, connection.CredentialRef); err == nil {
			request.SetBasicAuth(connection.Username, password)
		}
	}
	response, err := b.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return &BrowseError{Status: StatusUnreachable, Detail: "查询镜像仓库超时"}
		}
		return &BrowseError{Status: StatusUnreachable, Detail: fmt.Sprintf("查询镜像仓库失败：%v", err)}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return &BrowseError{Status: StatusAuthRequired, Detail: "仓库可达，但需要配置凭据才能浏览镜像"}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &BrowseError{Status: StatusDegraded, Detail: fmt.Sprintf("仓库返回 HTTP %d", response.StatusCode)}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(destination); err != nil {
		return &BrowseError{Status: StatusDegraded, Detail: fmt.Sprintf("解析仓库响应失败：%v", err)}
	}
	return nil
}

func escapeRepository(repository string) string {
	parts := strings.Split(repository, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
