package main

// 综合评分选股 RPC:否决+三层评分+状态门,快照与前向验证。

import (
	"sync"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/services"
)

var compositeOnce sync.Once

// composite 惰性初始化(评分需要 App.GetKLineData 的档案+实时衔接管线)
func (a *App) composite() *services.CompositeScoreService {
	compositeOnce.Do(func() {
		a.compositeScoreService = services.NewCompositeScoreService(
			a.historyService, a.intradayService, a.longHuBangService, a.GetKLineData)
	})
	return a.compositeScoreService
}

// RunCompositeScore 运行综合评分(preset: value=长线价值 / boom=中线景气),自动落当日快照
func (a *App) RunCompositeScore(preset string) models.CompositeScoreResult {
	if a == nil || a.historyService == nil {
		return models.CompositeScoreResult{Warning: "历史服务未就绪"}
	}
	return a.composite().Run(preset)
}

// GetCompositeValidation 快照前向验证报告(Top10 等权 30/60 交易日扣费超额)
func (a *App) GetCompositeValidation() models.CompositeValidationResult {
	if a == nil || a.historyService == nil {
		return models.CompositeValidationResult{Warning: "历史服务未就绪"}
	}
	return a.composite().Validate()
}
