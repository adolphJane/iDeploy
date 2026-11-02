package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"ideploy/internal/project"
)

type ProjectFileInput struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
}

type ProjectFileSaveInput struct {
	ProjectID    string `json:"projectId"`
	Kind         string `json:"kind"`
	Content      string `json:"content"`
	ExpectedHash string `json:"expectedHash"`
}

type ProjectFileView struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Hash     string `json:"hash"`
	Editable bool   `json:"editable"`
}

func (s *SystemService) ReadProjectFile(input ProjectFileInput) (ProjectFileView, error) {
	store, err := s.projectStore()
	if err != nil {
		return ProjectFileView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	value, err := store.Get(ctx, strings.TrimSpace(input.ProjectID))
	if err != nil {
		return ProjectFileView{}, err
	}
	relative, err := projectFileRelativePath(value, input.Kind)
	if err != nil {
		return ProjectFileView{}, err
	}
	root := value.LocalPath
	cleanup := func() {}
	if value.SourceKind == project.SourceGit {
		source, sourceErr := prepareReleaseSource(ctx, value, s.cacheDirectory(), discardJobReporter{})
		if sourceErr != nil {
			return ProjectFileView{}, fmt.Errorf("读取 Git 项目源码失败: %w", sourceErr)
		}
		root, cleanup = source.root, source.cleanup
	}
	defer cleanup()
	view, err := readProjectFileAtRoot(root, relative, input.Kind)
	view.Editable = value.SourceKind == project.SourceLocal
	return view, err
}

func (s *SystemService) SaveProjectFile(input ProjectFileSaveInput) (ProjectFileView, error) {
	store, err := s.projectStore()
	if err != nil {
		return ProjectFileView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := store.Get(ctx, strings.TrimSpace(input.ProjectID))
	if err != nil {
		return ProjectFileView{}, err
	}
	if value.SourceKind != project.SourceLocal {
		return ProjectFileView{}, errors.New("Git 项目文件请在源码仓库中修改并提交")
	}
	relative, err := projectFileRelativePath(value, input.Kind)
	if err != nil {
		return ProjectFileView{}, err
	}
	return saveProjectFileAtRoot(value.LocalPath, relative, input.Kind, input.Content, input.ExpectedHash)
}

func projectFileRelativePath(value project.Project, kind string) (string, error) {
	var relative string
	switch kind {
	case "dockerfile":
		relative = value.DockerfilePath
	case "compose":
		relative = value.ComposePath
	default:
		return "", errors.New("不支持的项目文件类型")
	}
	if strings.TrimSpace(relative) == "" {
		return "", errors.New("项目尚未配置该文件路径，请先编辑项目配置")
	}
	return relative, nil
}

func projectFilePath(root, relative string) (string, error) {
	path, err := secureProjectFile(root, relative)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(realRoot, realPath)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || inside == "." {
		return "", errors.New("项目文件路径超出项目目录")
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("只能查看和编辑普通文件")
	}
	return realPath, nil
}

func projectFileLimit(kind string) int64 {
	if kind == "dockerfile" {
		return 128 * 1024
	}
	return 1024 * 1024
}

func readProjectFileAtRoot(root, relative, kind string) (ProjectFileView, error) {
	path, err := projectFilePath(root, relative)
	if err != nil {
		return ProjectFileView{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return ProjectFileView{}, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, projectFileLimit(kind)+1))
	if err != nil {
		return ProjectFileView{}, err
	}
	if int64(len(content)) > projectFileLimit(kind) {
		return ProjectFileView{}, errors.New("文件过大，无法在应用内编辑")
	}
	if !utf8.Valid(content) || strings.ContainsRune(string(content), '\x00') {
		return ProjectFileView{}, errors.New("文件不是可编辑的 UTF-8 文本")
	}
	hash := sha256.Sum256(content)
	return ProjectFileView{Kind: kind, Path: relative, Content: string(content), Hash: hex.EncodeToString(hash[:])}, nil
}

func saveProjectFileAtRoot(root, relative, kind, content, expectedHash string) (ProjectFileView, error) {
	if expectedHash == "" {
		return ProjectFileView{}, errors.New("请先读取文件后再保存")
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') || int64(len(content)) > projectFileLimit(kind) {
		return ProjectFileView{}, errors.New("文件内容必须是有效 UTF-8 文本且不超过大小限制")
	}
	if kind == "dockerfile" {
		if err := validateDockerfileContent(content); err != nil {
			return ProjectFileView{}, err
		}
	} else if kind != "compose" || strings.TrimSpace(content) == "" {
		return ProjectFileView{}, errors.New("Compose 文件内容不能为空")
	}
	current, err := readProjectFileAtRoot(root, relative, kind)
	if err != nil {
		return ProjectFileView{}, err
	}
	if current.Hash != expectedHash {
		return ProjectFileView{}, errors.New("文件已被其他程序修改，请重新读取后再编辑")
	}
	path, err := projectFilePath(root, relative)
	if err != nil {
		return ProjectFileView{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return ProjectFileView{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ideploy-edit-*")
	if err != nil {
		return ProjectFileView{}, err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return ProjectFileView{}, err
	}
	if _, err := temporary.WriteString(content); err != nil {
		temporary.Close()
		return ProjectFileView{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return ProjectFileView{}, err
	}
	if err := temporary.Close(); err != nil {
		return ProjectFileView{}, err
	}
	latest, err := readProjectFileAtRoot(root, relative, kind)
	if err != nil || latest.Hash != expectedHash {
		return ProjectFileView{}, errors.New("保存前文件发生变化，请重新读取后再编辑")
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return ProjectFileView{}, err
	}
	view, err := readProjectFileAtRoot(root, relative, kind)
	view.Editable = true
	return view, err
}
