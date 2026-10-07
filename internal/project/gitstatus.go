package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GitStatus describes a local Git worktree. Ahead/behind is based on the
// locally recorded upstream ref unless refreshRemote is explicitly requested.
type GitStatus struct {
	Available    bool   `json:"available"`
	Detail       string `json:"detail"`
	Root         string `json:"root"`
	Branch       string `json:"branch"`
	Head         string `json:"head"`
	Upstream     string `json:"upstream"`
	ChangedFiles int    `json:"changedFiles"`
	StagedFiles  int    `json:"stagedFiles"`
	Untracked    int    `json:"untracked"`
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
	RemoteFresh  bool   `json:"remoteFresh"`
	CheckedAt    string `json:"checkedAt"`
}

func InspectGit(ctx context.Context, directory string, refreshRemote bool) GitStatus {
	result := GitStatus{CheckedAt: time.Now().Format(time.RFC3339)}
	root, err := gitOutput(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		result.Detail = "此项目目录不是 Git 工作区，或 Git 不可用"
		return result
	}
	result.Available = true
	result.Root = root
	result.Branch, _ = gitOutput(ctx, directory, "symbolic-ref", "--quiet", "--short", "HEAD")
	if result.Branch == "" {
		result.Branch = "分离 HEAD"
	}
	result.Head, _ = gitOutput(ctx, directory, "rev-parse", "--short", "HEAD")
	// NUL delimiters preserve filenames containing newlines. Renames/copies
	// include a second path record, which must not count as another change.
	porcelain, err := gitOutputRaw(ctx, directory, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		result.Detail = "读取本地改动失败：" + err.Error()
		return result
	}
	entries := strings.Split(strings.TrimSuffix(porcelain, "\x00"), "\x00")
	for index := 0; index < len(entries) && entries[index] != ""; index++ {
		entry := entries[index]
		if len(entry) < 3 {
			continue
		}
		if entry[:2] == "??" {
			result.Untracked++
		} else {
			result.ChangedFiles++
			if entry[0] != ' ' {
				result.StagedFiles++
			}
			if entry[0] == 'R' || entry[0] == 'C' || entry[1] == 'R' || entry[1] == 'C' {
				index++
			}
		}
	}
	result.Upstream, err = gitOutput(ctx, directory, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		result.Detail = "未设置上游分支；可查看本地改动，无法判断是否需要更新"
		return result
	}
	if refreshRemote {
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		_, err = gitOutput(fetchCtx, directory, "fetch", "--quiet", "--no-tags")
		if err != nil {
			result.Detail = "远端检查失败或超时；请检查网络与 Git 凭据。本地状态仍可用"
		} else {
			result.RemoteFresh = true
		}
	}
	counts, err := gitOutput(ctx, directory, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		result.Detail = "无法计算本地与上游分支的差异"
		return result
	}
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		result.Detail = "无法解析本地与上游分支的差异"
		return result
	}
	result.Ahead, _ = strconv.Atoi(fields[0])
	result.Behind, _ = strconv.Atoi(fields[1])
	if result.Detail == "" && !result.RemoteFresh {
		result.Detail = "远端差异依据本机上次获取的记录；点击“检查远端更新”可获取最新状态"
	}
	return result
}

func gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	output, err := gitOutputRaw(ctx, directory, args...)
	return strings.TrimSpace(output), err
}

func gitOutputRaw(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "SSH_ASKPASS=/usr/bin/false")
	output, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errors.New("连接超时")
		}
		message := strings.TrimSpace(string(output))
		if len(message) > 220 {
			message = message[:220] + "…"
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", message)
	}
	return string(output), nil
}
