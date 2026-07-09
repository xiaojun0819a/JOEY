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
