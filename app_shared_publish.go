package main

// 内部扫描结果发布通道(2026-07-31 建)。
//
// 起因:策略扫描结果的跨账号共享池 sharedScans 是在 **HTTP-RPC 处理器**里写的
// (main_headless.go 的 sharedScanPut),而后台定时扫描(14:40 那轮买点信号扫描)是 Go 内部
// 直接调 RunWaveScanner/RunTripleVolumeScannerV5,**根本不经过 HTTP**,所以结果从没进过共享池。
// 表现:自动扫描跑完,用户打开波段弹窗看到的仍是"上一次手动扫描"的旧结果和旧时间戳,
// 界面上完全看不出后台其实刚扫过一轮。
//
// 这里用一个函数变量做桥:headless 启动时注入实现(它能访问 sharedScans),
// 桌面版不注入(单机没有跨账号共享,nil 时调用是空操作)。
// 这样 app.go 这些无构建标签的共享代码不必知道 headless 的内部结构。

import "sync"

var (
	sharedPublishMu sync.RWMutex
	// sharedPublishFn 由 headless 入口注入。nil = 桌面版/未初始化,调用即空操作。
	sharedPublishFn func(method string, result any, user string)
)

// SetSharedScanPublisher 由 headless 入口注入发布实现。
func SetSharedScanPublisher(fn func(method string, result any, user string)) {
	sharedPublishMu.Lock()
	sharedPublishFn = fn
	sharedPublishMu.Unlock()
}

// sharedScanAutoUser 后台自动扫描在界面上的署名。
// 用户看到「来自"系统自动扫描"」就知道这轮不是自己点的,而是 14:40 定时跑的。
const sharedScanAutoUser = "系统自动扫描"

// publishSharedScan 把一次内部(非 RPC)扫描的结果放进跨账号共享池。
// 失败/未注入一律静默:发布只是让界面能看到,绝不能因为它出问题影响扫描与推送本身。
func publishSharedScan(method string, result any, user string) {
	sharedPublishMu.RLock()
	fn := sharedPublishFn
	sharedPublishMu.RUnlock()
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(method, result, user)
}
