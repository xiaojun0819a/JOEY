package main

// 竞价/分时数据查询 API(数据由 IntradayService 在 NAS 上实时采集自建)。

import (
	"github.com/run-bigpig/jcp/internal/services"
)

// GetAuctionFinal 某日竞价定型榜(9:25 撮合结果,按竞价金额降序)。date 形如 2026-07-06。
func (a *App) GetAuctionFinal(date string, limit int) []services.AuctionFinalRow {
	if a.intradayService == nil {
		return nil
	}
	rows, err := a.intradayService.AuctionFinal(date, limit)
	if err != nil {
		log.Warn("竞价定型查询失败 %s: %v", date, err)
		return nil
	}
	return rows
}

// StockIntradayResult 单股单日分时(含竞价过程)
type StockIntradayResult struct {
	Code    string                  `json:"code"`
	Date    string                  `json:"date"`
	Auction []services.IntradayTick `json:"auction"` // 9:15-9:25 竞价过程(30s粒度)
	Minutes []services.IntradayTick `json:"minutes"` // 9:30-15:00 分时(60s粒度,量额为累计)
}

// GetStockIntraday 某股某日的竞价过程+分时序列。
func (a *App) GetStockIntraday(code, date string) StockIntradayResult {
	res := StockIntradayResult{Code: code, Date: date}
	if a.intradayService == nil {
		return res
	}
	auction, minutes, err := a.intradayService.StockIntraday(code, date)
	if err != nil {
		log.Warn("分时查询失败 %s %s: %v", code, date, err)
		return res
	}
	res.Auction = auction
	res.Minutes = minutes
	return res
}

// GetStockFocusTicks 重点池 3 秒线(自选/持仓股)。
func (a *App) GetStockFocusTicks(code, date string) []services.IntradayTick {
	if a.intradayService == nil {
		return nil
	}
	ticks, err := a.intradayService.StockFocusTicks(code, date)
	if err != nil {
		return nil
	}
	return ticks
}

// intradayFocusPool 重点池:自选(全部分组去重)+ 模拟持仓在场股。
func (a *App) intradayFocusPool() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 128)
	add := func(sym string) {
		if sym == "" || seen[sym] {
			return
		}
		seen[sym] = true
		out = append(out, sym)
	}
	for _, codes := range a.GetStockGroups() {
		for _, c := range codes {
			add(c)
		}
	}
	if a.paperService != nil {
		for _, p := range a.paperService.OpenPositions() {
			add(p.Symbol)
		}
	}
	return out
}

// GetIntradayCoverage 采集覆盖统计(攒了几天/多少行)。
func (a *App) GetIntradayCoverage() services.IntradayCoverage {
	if a.intradayService == nil {
		return services.IntradayCoverage{}
	}
	return a.intradayService.Coverage()
}
