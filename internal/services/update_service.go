package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/blang/semver"
	"github.com/run-bigpig/go-github-selfupdate/selfupdate"
	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/rt"
)

var updateLog = logger.New("update")

// UpdateService 更新检测服务
// 负责从 GitHub Releases 检测和下载更新
type UpdateService struct {
	ctx            context.Context
	repoOwner      string // GitHub 仓库所有者
	repoName       string // GitHub 仓库名称
	currentVersion string // 当前版本号
}

// UpdateInfo 更新信息
type UpdateInfo struct {
	HasUpdate      bool   `json:"hasUpdate"`
	LatestVersion  string `json:"latestVersion"`
	CurrentVersion string `json:"currentVersion"`
	ReleaseURL     string `json:"releaseUrl"`
	ReleaseNotes   string `json:"releaseNotes"`
	Error          string `json:"error,omitempty"`
}

// UpdateProgress 更新进度信息
type UpdateProgress struct {
	Status  string `json:"status"`  // "checking", "downloading", "installing", "completed", "error"
	Message string `json:"message"` // 状态消息
	Percent int    `json:"percent"` // 进度百分比 (0-100)
}

// NewUpdateService 创建更新服务实例
func NewUpdateService(repoOwner, repoName, currentVersion string) *UpdateService {
	return &UpdateService{
		repoOwner:      repoOwner,
		repoName:       repoName,
		currentVersion: currentVersion,
	}
}

// Startup 在应用启动时调用
func (u *UpdateService) Startup(ctx context.Context) {
	u.ctx = ctx
	// 启动时清理旧文件
	if err := u.CleanupOldFiles(); err != nil {
		updateLog.Warn("清理旧文件失败: %v", err)
	}
}

// GetCurrentVersion 获取当前版本
func (u *UpdateService) GetCurrentVersion() string {
	return u.currentVersion
}

// CheckForUpdate 检查是否有可用更新
func (u *UpdateService) CheckForUpdate() UpdateInfo {
	repo := fmt.Sprintf("%s/%s", u.repoOwner, u.repoName)
	updateLog.Info("检查更新: repo=%s, current=%s", repo, u.currentVersion)

	latest, found, err := selfupdate.DetectLatest(repo)
	if err != nil {
		updateLog.Error("检测更新失败: %v", err)
		return UpdateInfo{
			HasUpdate:      false,
			CurrentVersion: u.currentVersion,
			Error:          fmt.Sprintf("检测更新失败: %v", err),
		}
	}

	if !found {
		return UpdateInfo{
			HasUpdate:      false,
			CurrentVersion: u.currentVersion,
			LatestVersion:  u.currentVersion,
			Error:          "未找到 GitHub Release",
		}
	}

	updateLog.Info("检测到版本: %s, URL: %s", latest.Version.String(), latest.URL)

	// 解析当前版本并比较
	currentVer, err := semver.ParseTolerant(u.currentVersion)
	if err != nil {
		hasUpdate := latest.Version.String() != u.currentVersion
		return UpdateInfo{
			HasUpdate:      hasUpdate,
			CurrentVersion: u.currentVersion,
			LatestVersion:  latest.Version.String(),
			ReleaseURL:     latest.URL,
			ReleaseNotes:   latest.ReleaseNotes,
			Error:          fmt.Sprintf("版本格式解析失败: %v", err),
		}
	}

	hasUpdate := latest.Version.GT(currentVer)
	return UpdateInfo{
		HasUpdate:      hasUpdate,
		CurrentVersion: u.currentVersion,
		LatestVersion:  latest.Version.String(),
		ReleaseURL:     latest.URL,
		ReleaseNotes:   latest.ReleaseNotes,
	}
}

// emitProgress 发送更新进度事件
func (u *UpdateService) emitProgress(status, message string, percent int) {
	if u.ctx == nil {
		return
	}
	progress := UpdateProgress{
		Status:  status,
		Message: message,
		Percent: percent,
	}
	rt.Emit("update:progress", progress)
}

// Update 执行更新（下载并替换当前可执行文件）
func (u *UpdateService) Update() error {
	u.emitProgress("checking", "正在检查更新...", 0)

	repo := fmt.Sprintf("%s/%s", u.repoOwner, u.repoName)

	u.emitProgress("checking", "正在检测最新版本...", 10)
	latest, found, err := selfupdate.DetectLatest(repo)
	if err != nil {
		u.emitProgress("error", fmt.Sprintf("检测更新失败: %v", err), 0)
		return fmt.Errorf("检测更新失败: %w", err)
	}

	if !found {
		u.emitProgress("error", "未找到更新", 0)
		return fmt.Errorf("未找到更新")
	}

	currentVer, err := semver.ParseTolerant(u.currentVersion)
	if err != nil {
		u.emitProgress("error", fmt.Sprintf("版本格式解析失败: %v", err), 0)
		return fmt.Errorf("版本格式解析失败: %w", err)
	}

	if !latest.Version.GT(currentVer) {
		u.emitProgress("error", "已是最新版本", 0)
		return fmt.Errorf("已是最新版本")
	}

	exe, err := os.Executable()
	if err != nil {
		u.emitProgress("error", fmt.Sprintf("获取可执行文件路径失败: %v", err), 0)
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 下载进度回调
	progressCallback := func(downloaded, total int64) {
		if total > 0 {
			downloadPercent := float64(downloaded) / float64(total)
			currentPercent := 30 + int(downloadPercent*40)
			downloadedMB := float64(downloaded) / (1024 * 1024)
			totalMB := float64(total) / (1024 * 1024)
			u.emitProgress("downloading",
				fmt.Sprintf("正在下载 %s... (%.2f MB / %.2f MB)",
					latest.Version.String(), downloadedMB, totalMB),
				currentPercent)
		} else {
			downloadedMB := float64(downloaded) / (1024 * 1024)
			u.emitProgress("downloading",
				fmt.Sprintf("正在下载 %s... (已下载 %.2f MB)",
					latest.Version.String(), downloadedMB),
				50)
		}
	}

	u.emitProgress("downloading", fmt.Sprintf("正在下载版本 %s...", latest.Version.String()), 30)

	if err := selfupdate.UpdateToWithProcess(latest.AssetURL, exe, progressCallback); err != nil {
		u.emitProgress("error", fmt.Sprintf("更新失败: %v", err), 0)
		return fmt.Errorf("更新失败: %w", err)
	}

	u.emitProgress("installing", "正在安装更新...", 90)
	u.emitProgress("completed", fmt.Sprintf("更新完成！新版本 %s 已安装", latest.Version.String()), 100)

	return nil
}

// RestartApplication 重启应用程序
func (u *UpdateService) RestartApplication() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	exePath, err := filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	updateLog.Info("准备重启应用: %s", exePath)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("powershell.exe", "-Command",
			fmt.Sprintf("Start-Sleep -Seconds 2; Start-Process -FilePath '%s'", exePath))
	case "darwin", "linux":
		cmd = exec.Command("sh", "-c", fmt.Sprintf("sleep 2 && %s", exePath))
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	cmd.Dir = filepath.Dir(exePath)
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}

	go func() {
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()

	return nil
}

// CleanupOldFiles 清理旧文件
func (u *UpdateService) CleanupOldFiles() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exe)
	currentExeAbs, _ := filepath.Abs(exe)

	patterns := []string{"*.old", "*.bak", "*.tmp"}
	cleanedCount := 0

	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(exeDir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			if filepath.Ext(match) == ".tmp" && time.Since(info.ModTime()) < time.Hour {
				continue
			}
			if err := os.Remove(match); err == nil {
				cleanedCount++
			}
		}
	}

	// 清理旧版本二进制
	exePattern := "jcp-*"
	if runtime.GOOS == "windows" {
		exePattern += ".exe"
	}
	matches, _ := filepath.Glob(filepath.Join(exeDir, exePattern))
	for _, match := range matches {
		matchAbs, _ := filepath.Abs(match)
		if matchAbs == currentExeAbs {
			continue
		}
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}
		if time.Since(info.ModTime()) > 7*24*time.Hour {
			if err := os.Remove(match); err == nil {
				cleanedCount++
			}
		}
	}

	updateLog.Info("清理完成，共清理 %d 个文件", cleanedCount)
	return nil
}
