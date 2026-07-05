// Package rt 是运行时垫片：把对 Wails runtime 的调用(事件/日志/窗口)抽象成可替换的函数变量。
// 桌面版(Wails)在启动时注入真实的 wails.runtime 实现；headless 服务器版注入 WebSocket/stdout 实现。
// 这样同一份 App 代码既能打包成桌面应用，也能编成无界面服务器跑在 NAS 上。
package rt

import (
	"fmt"
	"log"
)

type (
	emitFunc    = func(event string, data ...interface{})
	onFunc      = func(event string, cb func(...interface{}))
	logFunc     = func(msg string)
	logfFunc    = func(format string, args ...interface{})
	stringFunc  = func(s string)
	simpleFunc  = func()
)

var (
	emit          emitFunc   = func(string, ...interface{}) {}
	on            onFunc     = func(string, func(...interface{})) {}
	off           stringFunc = func(string) {}
	logInfof      logfFunc   = func(f string, a ...interface{}) { log.Printf("[INFO] "+f, a...) }
	logInfo       logFunc    = func(m string) { log.Println("[INFO]", m) }
	logError      logFunc    = func(m string) { log.Println("[ERROR]", m) }
	logDebug      logFunc    = func(m string) { log.Println("[DEBUG]", m) }
	logWarning    logFunc    = func(m string) { log.Println("[WARN]", m) }
	browserOpen   stringFunc = func(string) {}
	quit          simpleFunc = func() {}
	windowMin     simpleFunc = func() {}
	windowMax     simpleFunc = func() {}
	windowReload  simpleFunc = func() {}
	windowHide    simpleFunc = func() {}
	windowShow    simpleFunc = func() {}
)

// Wire 由桌面/headless 入口在启动时注入实现。传 nil 的保持默认。
type Impl struct {
	Emit         emitFunc
	On           onFunc
	Off          stringFunc
	LogInfof     logfFunc
	LogInfo      logFunc
	LogError     logFunc
	LogDebug     logFunc
	LogWarning   logFunc
	BrowserOpen  stringFunc
	Quit         simpleFunc
	WindowMin    simpleFunc
	WindowMax    simpleFunc
	WindowReload simpleFunc
	WindowHide   simpleFunc
	WindowShow   simpleFunc
}

func Wire(i Impl) {
	if i.Emit != nil {
		emit = i.Emit
	}
	if i.On != nil {
		on = i.On
	}
	if i.Off != nil {
		off = i.Off
	}
	if i.LogInfof != nil {
		logInfof = i.LogInfof
	}
	if i.LogInfo != nil {
		logInfo = i.LogInfo
	}
	if i.LogError != nil {
		logError = i.LogError
	}
	if i.LogDebug != nil {
		logDebug = i.LogDebug
	}
	if i.LogWarning != nil {
		logWarning = i.LogWarning
	}
	if i.BrowserOpen != nil {
		browserOpen = i.BrowserOpen
	}
	if i.Quit != nil {
		quit = i.Quit
	}
	if i.WindowMin != nil {
		windowMin = i.WindowMin
	}
	if i.WindowMax != nil {
		windowMax = i.WindowMax
	}
	if i.WindowReload != nil {
		windowReload = i.WindowReload
	}
	if i.WindowHide != nil {
		windowHide = i.WindowHide
	}
	if i.WindowShow != nil {
		windowShow = i.WindowShow
	}
}

func Emit(event string, data ...interface{}) { emit(event, data...) }
func On(event string, cb func(...interface{})) { on(event, cb) }
func Off(event string)                          { off(event) }
func LogInfof(format string, args ...interface{}) { logInfof(format, args...) }
func LogInfo(msg string)                          { logInfo(msg) }
func LogError(msg string)                         { logError(msg) }
func LogDebug(msg string)                         { logDebug(msg) }
func LogWarning(msg string)                       { logWarning(msg) }
func BrowserOpenURL(url string)                   { browserOpen(url) }
func Quit()                                       { quit() }
func WindowMinimise()                             { windowMin() }
func WindowToggleMaximise()                       { windowMax() }
func WindowReload()                               { windowReload() }
func Hide()                                       { windowHide() }
func Show()                                       { windowShow() }

// Sprintf 便捷(供 headless 日志实现拼接)。
func Sprintf(format string, a ...interface{}) string { return fmt.Sprintf(format, a...) }
