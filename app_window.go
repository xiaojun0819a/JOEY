package main

// 多窗口(2026-07-26 用户提:"可以打开一个新软件的窗口")。
// Wails v2 是单窗口架构(多窗口要到 v3),所以"新窗口"= 再起一个应用实例(独立进程)。
// 实测两个实例并存干净:都走远程模式瘦身(不重复启调度器/采集),各自连 NAS,本地库 WAL 多进程可读写。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// OpenNewWindow 再开一个软件窗口(新实例)。必须在本机执行:
// remoteBridge 的 LOCAL_METHODS 白名单里有它,不会被转发到 NAS;
// 这里再用 allowRemoteBackend 兜一道——headless 后端恒 false,防止有人直接打 RPC 让服务器起进程。
func (a *App) OpenNewWindow() string {
	if !allowRemoteBackend {
		return "服务器端不支持开窗口"
	}
	exe, err := os.Executable()
	if err != nil {
		return "取可执行文件路径失败:" + err.Error()
	}
	exePath, err := filepath.Abs(exe)
	if err != nil {
		return "取绝对路径失败:" + err.Error()
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS 必须 `open -n -a <.app>`:直接跑包内二进制不会被 LaunchServices 当成独立 app,
		// 拿不到自己的 Dock 图标和窗口焦点。从 .../Xxx.app/Contents/MacOS/jcp 反推出包路径。
		bundle := exePath
		if i := strings.Index(exePath, ".app/Contents/MacOS/"); i > 0 {
			bundle = exePath[:i+len(".app")]
		}
		cmd = exec.Command("open", "-n", "-a", bundle)
	case "windows":
		cmd = exec.Command(exePath)
	default:
		cmd = exec.Command(exePath)
	}
	cmd.Dir = filepath.Dir(exePath)
	// 不需要额外脱离父进程:macOS 由 LaunchServices 接管(open 立刻退出,新实例是它的孩子),
	// Windows 的子进程也不随父进程退出而被杀。所以关掉这个窗口不会连带关掉新窗口。
	if err := cmd.Start(); err != nil {
		return "打开新窗口失败:" + err.Error()
	}
	log.Info("已打开新窗口实例: %s", exePath)
	return "success"
}

// CountAppInstances 当前本机跑着几个本软件实例(设置页展示用,失败回 0 不报错)。
func (a *App) CountAppInstances() int {
	if !allowRemoteBackend {
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	out, err := exec.Command("sh", "-c", fmt.Sprintf("pgrep -f %q | wc -l", filepath.Base(exe))).Output()
	if err != nil {
		return 0
	}
	n := 0
	_, _ = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n)
	return n
}
