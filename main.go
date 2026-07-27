//go:build !headless

package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 捕获 panic 并写入日志文件
	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
		}
	}()

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "观鲸测浪",
		Width:     1920,
		Height:    1080,
		MinWidth:  1366,
		MinHeight: 768,
		// macOS 原生全屏(绿灯行为)要求窗口带 NSWindowStyleMaskTitled,而 Frameless 会把它去掉,
		// 导致 toggleFullScreen: 静默失效(2026-07-27 实测点了没反应,已在 Wails v2.11 源码坐实)。
		// 改用 TitleBarHidden:标题栏透明、标题隐藏、内容铺满,但**保留 Titled**,所以全屏可用。
		// 代价=左上角出现系统红黄绿三个灯,故前端在 macOS 上隐藏自绘的那三个按钮并给顶栏留左侧空位。
		// 回退方式:把下面 Mac 那段删掉,改回 Frameless: true,并把 App.tsx 里 IS_MAC 相关的三处还原。
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHidden(),
		},
		CSSDragProperty: "--wails-draggable",
		CSSDragValue:    "drag",
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// logPanic 将 panic 信息写入日志文件
func logPanic(r interface{}) {
	// 获取可执行文件所在目录
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	logFile := filepath.Join(dir, "crash.log")

	// 写入崩溃日志
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	msg := fmt.Sprintf("PANIC: %v\n%s\n", r, debug.Stack())
	f.WriteString(msg)
}
