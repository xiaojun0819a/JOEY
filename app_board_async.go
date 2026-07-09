package main

// 老陈完整报告(看板报告)异步化:生成要 3-8 分钟,同步 RPC 经 Cloudflare 隧道会被
// ~100 秒"无响应超时"掐断(Windows 走公网必中,Mac 走内网无此限)。改成"启动 + 轮询":
// StartBoardReport 秒回,后台生成;前端每几秒 GetBoardReportStatus 拉进度——每次请求都很快,隧道不掐。

import (
	"strings"
	"sync"
	"time"
)

type boardJob struct {
	mu    sync.Mutex
	start time.Time
	done  bool
	resp  GenerateBoardReportResponse
}

var (
	boardJobsMu sync.Mutex
	boardJobs   = map[string]*boardJob{}
)

// boardJobKey 带用户前缀,避免 headless 多用户分身共享的 job map 串号。
func (a *App) boardJobKey(stockCode, period string) string {
	return a.guestUsername + "|" + strings.TrimSpace(stockCode) + "|" + strings.TrimSpace(period)
}

// BoardReportStatus 异步看板报告状态。
type BoardReportStatus struct {
	Status      string `json:"status"` // running | done | failed | idle
	ElapsedSec  int    `json:"elapsedSec,omitempty"`
	StockCode   string `json:"stockCode,omitempty"`
	StockName   string `json:"stockName,omitempty"`
	Report      string `json:"report,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	ModelName   string `json:"modelName,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty"`
	FromCache   bool   `json:"fromCache,omitempty"`
	Error       string `json:"error,omitempty"`
}

// StartBoardReport 异步启动生成。已在跑则返回 running(幂等,重复点不会重复触发)。
func (a *App) StartBoardReport(req GenerateBoardReportRequest) BoardReportStatus {
	stockCode := strings.TrimSpace(req.StockCode)
	if stockCode == "" {
		return BoardReportStatus{Status: "failed", Error: "stockCode 不能为空"}
	}
	key := a.boardJobKey(stockCode, req.Period)

	boardJobsMu.Lock()
	if job, ok := boardJobs[key]; ok {
		job.mu.Lock()
		running := !job.done
		elapsed := int(time.Since(job.start).Seconds())
		job.mu.Unlock()
		if running {
			boardJobsMu.Unlock()
			return BoardReportStatus{Status: "running", StockCode: stockCode, ElapsedSec: elapsed}
		}
	}
	job := &boardJob{start: time.Now()}
	boardJobs[key] = job
	boardJobsMu.Unlock()

	go func() {
		resp := a.GenerateBoardReport(req) // 复用同步实现(含落库缓存 SaveBoardReport)
		job.mu.Lock()
		job.resp = resp
		job.done = true
		job.mu.Unlock()
	}()

	return BoardReportStatus{Status: "running", StockCode: stockCode, ElapsedSec: 0}
}

// GetBoardReportStatus 轮询状态。每次调用都很快,适合经隧道频繁轮询。
// 无进行中任务时回落查缓存——之前生成过就直接返回(重开弹窗秒显)。
func (a *App) GetBoardReportStatus(stockCode, period string) BoardReportStatus {
	stockCode = strings.TrimSpace(stockCode)
	key := a.boardJobKey(stockCode, period)

	boardJobsMu.Lock()
	job, ok := boardJobs[key]
	boardJobsMu.Unlock()

	if ok {
		job.mu.Lock()
		defer job.mu.Unlock()
		if !job.done {
			return BoardReportStatus{Status: "running", StockCode: stockCode, ElapsedSec: int(time.Since(job.start).Seconds())}
		}
		r := job.resp
		if !r.Success {
			return BoardReportStatus{Status: "failed", StockCode: stockCode, Error: r.Error}
		}
		return BoardReportStatus{
			Status: "done", StockCode: r.StockCode, StockName: r.StockName, Report: r.Report,
			AgentID: r.AgentID, AgentName: r.AgentName, ModelName: r.ModelName, GeneratedAt: r.GeneratedAt,
		}
	}

	// 无任务:查缓存
	if c := a.GetCachedBoardReport(stockCode, period); c.Found {
		return BoardReportStatus{
			Status: "done", StockCode: c.StockCode, StockName: c.StockName, Report: c.Report,
			AgentID: c.AgentID, AgentName: c.AgentName, ModelName: c.ModelName,
			GeneratedAt: c.GeneratedAt, FromCache: true,
		}
	}
	return BoardReportStatus{Status: "idle", StockCode: stockCode}
}
