package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

// RuntimeMount is the storage metadata exposed by a deployment inspection.
type RuntimeMount struct {
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	Name        string `json:"name,omitempty"`
	RW          bool   `json:"readWrite"`
}

type ContainerRuntimeDetails struct {
	ID          string         `json:"id"`
	ImageID     string         `json:"imageId"`
	ImageRef    string         `json:"imageRef"`
	Service     string         `json:"service,omitempty"`
	ReleaseID   string         `json:"releaseId,omitempty"`
	Version     string         `json:"version,omitempty"`
	PinnedImage string         `json:"pinnedImage,omitempty"`
	Mounts      []RuntimeMount `json:"mounts"`
}

// Use a projection so Docker does not send Config.Env, unfiltered labels,
// or the full inspect document to the desktop.
const runtimeDetailFormat = `{{- $labels := index .Config "Labels" -}}{"id":{{json .Id}},"imageId":{{json .Image}},"imageRef":{{json .Config.Image}},"service":{{if $labels}}{{json (index $labels "com.docker.compose.service")}}{{else}}null{{end}},"releaseId":{{if $labels}}{{json (index $labels "com.ideploy.release-id")}}{{else}}null{{end}},"version":{{if $labels}}{{json (index $labels "com.ideploy.version")}}{{else}}null{{end}},"pinnedImage":{{if $labels}}{{json (index $labels "com.ideploy.pinned-image")}}{{else}}null{{end}},"mounts":[{{range $idx,$mount := .Mounts}}{{if $idx}},{{end}}{"type":{{json (index $mount "Type")}},"source":{{json (index $mount "Source")}},"destination":{{json (index $mount "Destination")}},"name":{{json (index $mount "Name")}},"readWrite":{{json (index $mount "RW")}}}{{end}}]}`

var fullContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var fullImageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var namedVolume = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,254}$`)

type ContainerMountSpec struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

func (m ContainerMountSpec) Argument() (string, error) {
	validPath := func(value string) bool {
		return ValidRuntimeDirectory(value) && !strings.ContainsAny(value, ",\"")
	}
	if !validPath(m.Destination) || !(m.Type == "bind" && validPath(m.Source) || m.Type == "volume" && namedVolume.MatchString(m.Source)) {
		return "", errors.New("无效的容器持久化挂载")
	}
	argument := "type=" + m.Type + ",source=" + m.Source + ",target=" + m.Destination
	if m.Type == "volume" {
		argument += ",volume-nocopy"
	}
	if m.ReadOnly {
		argument += ",readonly"
	}
	return argument, nil
}

func (r *Remote) CheckMountSource(ctx context.Context, connection Connection, mount ContainerMountSpec) error {
	if _, err := mount.Argument(); err != nil {
		return err
	}
	command := "test -e " + shellQuote(mount.Source)
	if mount.Type == "volume" {
		command = "docker volume inspect --format '{{.Name}}' -- " + shellQuote(mount.Source)
	}
	output, err := r.runTimeout(ctx, connection, command, 10*time.Second)
	if err != nil || mount.Type == "volume" && strings.TrimSpace(output) != mount.Source {
		return errors.New("挂载源不存在或无法确认")
	}
	return nil
}

func (r *Remote) InspectContainerRuntime(ctx context.Context, connection Connection, ids []string) ([]ContainerRuntimeDetails, error) {
	details := make([]ContainerRuntimeDetails, 0, len(ids))
	if len(ids) == 0 {
		return details, nil
	}
	if len(ids) > 64 {
		return nil, errors.New("单次部署检查最多支持 64 个容器")
	}
	expected := make(map[string]bool, len(ids))
	arguments := make([]string, 0, len(ids))
	for _, id := range ids {
		if !fullContainerID.MatchString(id) || expected[id] {
			return nil, errors.New("容器检查要求不同的完整容器 ID")
		}
		expected[id] = true
		arguments = append(arguments, shellQuote(id))
	}
	output, err := r.runTimeout(ctx, connection, "docker container inspect --format "+shellQuote(runtimeDetailFormat)+" -- "+strings.Join(arguments, " "), 20*time.Second)
	if err != nil {
		return nil, errors.New("无法读取目标容器的镜像或挂载，容器可能刚被替换")
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var detail ContainerRuntimeDetails
		if err := json.Unmarshal([]byte(line), &detail); err != nil || !expected[detail.ID] || !fullImageID.MatchString(detail.ImageID) {
			return nil, errors.New("Docker 返回的容器检查结果无效")
		}
		delete(expected, detail.ID)
		details = append(details, detail)
	}
	if len(expected) != 0 {
		return nil, errors.New("Docker 返回的容器检查结果不完整")
	}
	return details, nil
}

// InspectLocalImageID resolves a pinned reference using the target daemon's
// local image store. It never pulls or modifies the image.
func (r *Remote) InspectLocalImageID(ctx context.Context, connection Connection, pinnedRef string) (string, error) {
	_, digest, ok := strings.Cut(pinnedRef, "@")
	if !ok || !fullImageID.MatchString(digest) || validateImageReference(pinnedRef) != nil {
		return "", errors.New("镜像引用必须包含有效 Digest")
	}
	output, err := r.runTimeout(ctx, connection, "docker image inspect --format '{{.Id}}' -- "+shellQuote(pinnedRef), 12*time.Second)
	if err != nil || !fullImageID.MatchString(strings.TrimSpace(output)) {
		return "", errors.New("无法在目标 Docker 上解析发布制品的镜像 ID")
	}
	return strings.TrimSpace(output), nil
}

func ValidRuntimeDirectory(directory string) bool {
	return len(directory) > 1 && len(directory) <= 1024 && path.IsAbs(directory) &&
		path.Clean(directory) == directory && !strings.ContainsAny(directory, "\x00\r\n")
}

// CheckContainerDirectory runs the container's test utility as its default
// runtime user. An unavailable utility or a stopped container is inconclusive.
func (r *Remote) CheckContainerDirectory(ctx context.Context, connection Connection, id, directory, access string) (string, error) {
	if !fullContainerID.MatchString(id) || !ValidRuntimeDirectory(directory) {
		return "", errors.New("无效的容器数据目录")
	}
	test := func(flag string) string {
		return "docker exec " + shellQuote(id) + " test " + flag + " " + shellQuote(directory)
	}
	probe := "printf directory"
	switch access {
	case "":
	case "readable":
		probe = directoryAccessProbe(test("-r"), test("-x"))
	case "writable":
		probe = directoryAccessProbe(test("-w"), test("-x"))
	default:
		return "", errors.New("无效的数据目录访问检查")
	}
	command := "result=$(" + test("-d") + " 2>&1); status=$?; if [ \"$status\" -eq 0 ]; then " + probe +
		"; elif [ \"$status\" -eq 1 ] && [ -z \"$result\" ]; then printf unavailable; else printf unknown; fi"
	output, err := r.runTimeout(ctx, connection, command, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("无法读取容器数据目录状态")
	}
	switch result := strings.TrimSpace(output); result {
	case "directory", "unavailable", "inaccessible", "unknown":
		return result, nil
	default:
		return "", errors.New("Docker 返回了无法识别的目录状态")
	}
}

func directoryAccessProbe(access, searchable string) string {
	return "result=$(" + access + " 2>&1 && " + searchable + " 2>&1); status=$?; " +
		"if [ \"$status\" -eq 0 ]; then printf directory; elif [ \"$status\" -eq 1 ] && [ -z \"$result\" ]; then printf inaccessible; else printf unknown; fi"
}
