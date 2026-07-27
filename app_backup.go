package main

// 数据备份 RPC:自动备份由权威后端每日 03:30 跑(backup_service),这里提供手动触发与状态查询。

import (
	"sync"

	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/services"
)

var backupOnce sync.Once

func (a *App) backup() *services.BackupService {
	backupOnce.Do(func() {
		a.backupService = services.NewBackupService(paths.GetDataDir())
	})
	return a.backupService
}

// RunBackupNow 立即执行一次备份(含 intraday 周备可选:weekly=true 时附带)
func (a *App) RunBackupNow(weekly bool) services.BackupResult {
	return a.backup().RunBackup(weekly)
}

// GetBackupStatus 最近一次备份结果(可能为 nil=从未备份)
func (a *App) GetBackupStatus() *services.BackupResult {
	return a.backup().Status()
}

// RunQfqRebuild 全市场前复权重建(重活,手动触发):把 stock_daily 最近420根改写为行情源前复权口径,
// 消除本地不复权+缺OHL的双口径问题。日常维护由每日采集的除权检测自动做,本接口用于一次性对齐/大修。
func (a *App) RunQfqRebuild(concurrency int) services.RebuildQfqResult {
	if a.historyService == nil {
		return services.RebuildQfqResult{Message: "历史服务未初始化"}
	}
	return a.historyService.RebuildAllQfq(concurrency, 420)
}
