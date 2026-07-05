//go:build !headless

package main

import (
	"context"

	"github.com/run-bigpig/jcp/internal/rt"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 桌面版：把 rt 垫片接到真实的 Wails runtime（需要 startup 传入的 ctx）。
func init() {
	// 仅桌面版允许"探到 NAS 就进瘦身模式"；headless 后端不含本文件，保持 false。
	allowRemoteBackend = true
	runtimeWirer = func(ctx context.Context) {
		rt.Wire(rt.Impl{
			Emit:         func(e string, d ...interface{}) { wr.EventsEmit(ctx, e, d...) },
			On:           func(e string, cb func(...interface{})) { wr.EventsOn(ctx, e, cb) },
			Off:          func(e string) { wr.EventsOff(ctx, e) },
			LogInfof:     func(f string, a ...interface{}) { wr.LogInfof(ctx, f, a...) },
			LogInfo:      func(m string) { wr.LogInfo(ctx, m) },
			LogError:     func(m string) { wr.LogError(ctx, m) },
			LogDebug:     func(m string) { wr.LogDebug(ctx, m) },
			LogWarning:   func(m string) { wr.LogWarning(ctx, m) },
			BrowserOpen:  func(u string) { wr.BrowserOpenURL(ctx, u) },
			Quit:         func() { wr.Quit(ctx) },
			WindowMin:    func() { wr.WindowMinimise(ctx) },
			WindowMax:    func() { wr.WindowToggleMaximise(ctx) },
			WindowReload: func() { wr.WindowReload(ctx) },
			WindowHide:   func() { wr.Hide(ctx) },
			WindowShow:   func() { wr.Show(ctx) },
		})
	}
}
