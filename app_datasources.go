package main

import (
	"github.com/run-bigpig/jcp/internal/services"
)

// 外部数据源 RPC(股吧舆情/市场情绪面/巨潮公告)。
// 服务用包级单例,不占 App 字段,便于与其他改动并行;方法均只读,访客可用。

// GetGubaSentiment 东财股吧个股热帖(散户情绪/题材发酵线索)
func (a *App) GetGubaSentiment(code string, limit int) (services.GubaSummary, error) {
	return services.DefaultGubaService().GetGubaSummary(code, limit)
}

// GetMarketMood 全市场情绪面:涨跌家数分布/两融余额5日/行业板块涨跌榜
func (a *App) GetMarketMood() (services.MarketMood, error) {
	return services.DefaultMarketMoodService().GetMarketMood()
}

// GetCninfoAnnouncements 巨潮官方公告查询(searchKey 可选,如"问询函"/"减持")
func (a *App) GetCninfoAnnouncements(code string, searchKey string, limit int) (services.CninfoResult, error) {
	return services.DefaultCninfoService().QueryAnnouncements(code, searchKey, limit)
}
