package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/inconshreveable/go-update"
	"github.com/run-bigpig/jcp/internal/logger"
	"github.com/run-bigpig/jcp/internal/rt"
)

var updateLog = logger.New("update")

// 默认更新源(NAS 公网隧道)。可被 config.RemoteBackendPublicURL 覆盖。
const defaultUpdateBase = "https://joey-app.junai.uk"

// UpdateService 更新检测服务
// 从自建 NAS(joey-app.junai.uk)拉版本清单和安装包,不走 GitHub(国内常连不上导致卡死)。
type UpdateService struct {
	ctx            context.Context
	currentVersion string           // 当前版本号
	baseURLFn      func() string     // 返回更新源基址(默认公网隧道,可被 config 覆盖)
}

// updateManifest NAS 上的版本清单(/update/manifest.json)。
// assets 按 "<GOOS>/<GOARCH>" 索引到全量安装包文件名(相对 /update/)。
// patches 按 "<GOOS>/<GOARCH>" → "<当前版本>" 索引到增量补丁文件名——
// 客户端当前版本有对应补丁时只下补丁(几百KB~几MB),否则回落下全量包。
type updateManifest struct {
	Version string                       `json:"version"`
	Notes   string                       `json:"notes"`
	Assets  map[string]string            `json:"assets"`
	Patches map[string]map[string]string `json:"patches,omitempty"`
}

// assetKey 当前平台在清单里的键。
func assetKey() string { return runtime.GOOS + "/" + runtime.GOARCH }

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

// NewUpdateService 创建更新服务实例。baseURLFn 返回更新源基址(为空时用默认公网隧道)。
func NewUpdateService(currentVersion string, baseURLFn func() string) *UpdateService {
	return &UpdateService{
		currentVersion: currentVersion,
		baseURLFn:      baseURLFn,
	}
}

// baseURL 解析当前更新源基址。
func (u *UpdateService) baseURL() string {
	base := ""
	if u.baseURLFn != nil {
		base = strings.TrimSpace(u.baseURLFn())
	}
	if base == "" {
		base = defaultUpdateBase
	}
	return strings.TrimRight(base, "/")
}

// fetchManifest 拉取 NAS 版本清单。
func (u *UpdateService) fetchManifest() (*updateManifest, error) {
	url := u.baseURL() + "/update/manifest.json"
	client := &http.Client{Timeout: 12 * time.Second} // 有超时,连不上会明确失败而非无限卡住
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("连接更新服务器失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新服务器返回 %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("解析版本清单失败: %w", err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("版本清单缺少 version 字段")
	}
	return &m, nil
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
	updateLog.Info("检查更新: source=%s, current=%s", u.baseURL(), u.currentVersion)

	m, err := u.fetchManifest()
	if err != nil {
		updateLog.Error("检测更新失败: %v", err)
		return UpdateInfo{
			HasUpdate:      false,
			CurrentVersion: u.currentVersion,
			Error:          err.Error(),
		}
	}

	// 当前平台有没有对应安装包
	if _, ok := m.Assets[assetKey()]; !ok {
		return UpdateInfo{
			HasUpdate:      false,
			CurrentVersion: u.currentVersion,
			LatestVersion:  m.Version,
			ReleaseNotes:   m.Notes,
			Error:          "该版本暂无当前系统(" + assetKey() + ")的安装包",
		}
	}

	updateLog.Info("检测到版本: %s", m.Version)

	hasUpdate := false
	if cur, err := semver.ParseTolerant(u.currentVersion); err == nil {
		if latest, err2 := semver.ParseTolerant(m.Version); err2 == nil {
			hasUpdate = latest.GT(cur)
		} else {
			hasUpdate = m.Version != u.currentVersion
		}
	} else {
		// 当前版本号非标准(如 dev):版本串不同即认为可更新
		hasUpdate = m.Version != u.currentVersion
	}

	return UpdateInfo{
		HasUpdate:      hasUpdate,
		CurrentVersion: u.currentVersion,
		LatestVersion:  m.Version,
		ReleaseURL:     u.baseURL() + "/download",
		ReleaseNotes:   m.Notes,
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

// Update 执行更新：从 NAS 下载当前平台安装包并原地替换可执行文件。
func (u *UpdateService) Update() error {
	u.emitProgress("checking", "正在检查更新...", 10)

	m, err := u.fetchManifest()
	if err != nil {
		u.emitProgress("error", err.Error(), 0)
		return err
	}

	asset, ok := m.Assets[assetKey()]
	if !ok || asset == "" {
		msg := "该版本暂无当前系统(" + assetKey() + ")的安装包"
		u.emitProgress("error", msg, 0)
		return fmt.Errorf("%s", msg)
	}

	// 已是最新则不动(版本可比时)
	if cur, e1 := semver.ParseTolerant(u.currentVersion); e1 == nil {
		if latest, e2 := semver.ParseTolerant(m.Version); e2 == nil && !latest.GT(cur) {
			u.emitProgress("error", "已是最新版本", 0)
			return fmt.Errorf("已是最新版本")
		}
	}

	// 优先增量更新:当前版本有对应补丁则只下补丁(小、快),apply 时打到现有 exe 上。
	if patch, ok := m.Patches[assetKey()][u.currentVersion]; ok && patch != "" {
		if err := u.applyDownload(m.Version, patch, true); err == nil {
			u.emitProgress("completed", fmt.Sprintf("更新完成！新版本 %s 已安装,重启后生效", m.Version), 100)
			return nil
		} else {
			// 补丁失败(exe 被改过/补丁不匹配)→ 回落全量,不让用户卡住
			updateLog.Warn("增量补丁失败,回落全量下载: %v", err)
			u.emitProgress("downloading", "增量更新不适用,改为完整下载...", 30)
		}
	}

	// 全量下载安装
	if err := u.applyDownload(m.Version, asset, false); err != nil {
		return err
	}
	u.emitProgress("completed", fmt.Sprintf("更新完成！新版本 %s 已安装,重启后生效", m.Version), 100)
	return nil
}

// applyDownload 下载 name(相对 /update/ 或完整 URL)并 apply。isPatch=true 时按 bsdiff 补丁打到现有 exe。
func (u *UpdateService) applyDownload(version, name string, isPatch bool) error {
	url := name
	if !strings.HasPrefix(name, "http") {
		url = u.baseURL() + "/update/" + strings.TrimLeft(name, "/")
	}
	label := "完整包"
	if isPatch {
		label = "增量补丁"
	}
	u.emitProgress("downloading", fmt.Sprintf("正在下载%s (v%s)...", label, version), 30)

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		u.emitProgress("error", "下载失败: "+err.Error(), 0)
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败,服务器返回 %d", resp.StatusCode)
	}

	total := resp.ContentLength
	pr := &progressReader{
		reader: resp.Body,
		total:  total,
		onProgress: func(read int64) {
			pct := 30
			msg := fmt.Sprintf("正在下载%s... (%.1f MB)", label, float64(read)/(1024*1024))
			if total > 0 {
				pct = 30 + int(float64(read)/float64(total)*55)
				msg = fmt.Sprintf("正在下载%s... (%.1f / %.1f MB)", label,
					float64(read)/(1024*1024), float64(total)/(1024*1024))
			}
			u.emitProgress("downloading", msg, pct)
		},
	}

	u.emitProgress("installing", "正在安装更新...", 88)
	opts := update.Options{}
	if isPatch {
		opts.Patcher = update.NewBSDiffPatcher() // 把补丁打到当前正在运行的 exe 上
	}
	if err := update.Apply(pr, opts); err != nil {
		if rerr := update.RollbackError(err); rerr != nil {
			u.emitProgress("error", "更新失败且回滚失败: "+rerr.Error(), 0)
			return fmt.Errorf("更新失败且回滚失败: %w", rerr)
		}
		return fmt.Errorf("安装失败: %w", err)
	}
	return nil
}

// progressReader 包装下载流,边读边回报进度。
type progressReader struct {
	reader     io.Reader
	total      int64
	read       int64
	onProgress func(read int64)
	lastEmit   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	p.read += int64(n)
	// 限流:最多 ~5 次/秒,避免事件刷屏
	if p.onProgress != nil && time.Since(p.lastEmit) > 200*time.Millisecond {
		p.onProgress(p.read)
		p.lastEmit = time.Now()
	}
	return n, err
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
