package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type AppUpdateProgress struct {
	Phase           string `json:"phase"`
	Detail          string `json:"detail"`
	Percent         int    `json:"percent"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	FilePath        string `json:"filePath"`
}

var appDownload = struct {
	sync.Mutex
	active   bool
	progress AppUpdateProgress
}{}

func (s *SystemService) GetAppUpdateProgress() (AppUpdateProgress, error) {
	appDownload.Lock()
	progress := appDownload.progress
	appDownload.Unlock()
	if progress.Phase != "" && progress.Phase != "idle" {
		return progress, nil
	}
	return sparkleUpdateProgress()
}

func updateDownloadProgress(progress AppUpdateProgress) {
	appDownload.Lock()
	appDownload.progress = progress
	appDownload.Unlock()
}

func appUpdateAssetName(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || !strings.HasPrefix(parsed.Path, "/adolphJane/iDeploy/releases/download/") {
		return "", errors.New("更新包地址无效")
	}
	name := path.Base(parsed.Path)
	if !strings.HasPrefix(name, "iDeploy-") || !(strings.HasSuffix(name, ".dmg") || strings.HasSuffix(name, ".zip")) {
		return "", errors.New("更新包文件名无效")
	}
	return name, nil
}

type updateDownloadWriter struct {
	writer   io.Writer
	received int64
	total    int64
}

func (writer *updateDownloadWriter) Write(data []byte) (int, error) {
	n, err := writer.writer.Write(data)
	writer.received += int64(n)
	percent := -1
	if writer.total > 0 {
		percent = int(min(99, writer.received*100/writer.total))
	}
	updateDownloadProgress(AppUpdateProgress{
		Phase: "downloading", Detail: "正在下载更新包", Percent: percent,
		DownloadedBytes: writer.received, TotalBytes: writer.total,
	})
	return n, err
}

func (s *SystemService) DownloadAppUpdate(downloadURL string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("本地更新包下载仅支持 macOS")
	}
	name, err := appUpdateAssetName(downloadURL)
	if err != nil {
		return "", err
	}
	appDownload.Lock()
	if appDownload.active {
		appDownload.Unlock()
		return "", errors.New("更新包正在下载")
	}
	appDownload.active = true
	appDownload.progress = AppUpdateProgress{Phase: "downloading", Detail: "正在连接 GitHub", Percent: -1}
	appDownload.Unlock()
	defer func() {
		appDownload.Lock()
		appDownload.active = false
		appDownload.Unlock()
	}()
	fail := func(err error) (string, error) {
		updateDownloadProgress(AppUpdateProgress{Phase: "failed", Detail: err.Error(), Percent: -1})
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fail(err)
	}
	request.Header.Set("User-Agent", "iDeploy/"+appVersion)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fail(fmt.Errorf("下载更新包失败: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("下载更新包失败：HTTP %d", response.StatusCode))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	downloads := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloads, 0o700); err != nil {
		return fail(err)
	}
	extension := filepath.Ext(name)
	temporary, err := os.CreateTemp(downloads, strings.TrimSuffix(name, extension)+"-*.partial")
	if err != nil {
		return fail(err)
	}
	defer os.Remove(temporary.Name())
	writer := &updateDownloadWriter{writer: temporary, total: response.ContentLength}
	if _, err := io.Copy(writer, response.Body); err != nil {
		temporary.Close()
		return fail(err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fail(err)
	}
	if err := temporary.Close(); err != nil {
		return fail(err)
	}
	destination := strings.TrimSuffix(temporary.Name(), ".partial") + extension
	if err := os.Rename(temporary.Name(), destination); err != nil {
		return fail(err)
	}
	updateDownloadProgress(AppUpdateProgress{
		Phase: "downloaded", Detail: "下载完成，已在 Finder 中打开安装包", Percent: 100,
		DownloadedBytes: writer.received, TotalBytes: writer.received, FilePath: destination,
	})
	if err := exec.Command("open", destination).Run(); err != nil {
		updateDownloadProgress(AppUpdateProgress{
			Phase: "downloaded", Detail: "下载完成，请在下载目录打开安装包", Percent: 100,
			DownloadedBytes: writer.received, TotalBytes: writer.received, FilePath: destination,
		})
	}
	return destination, nil
}
