package tools

import (
	"fmt"
	"math"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// GetKLineInput K线数据输入参数
type GetKLineInput struct {
	Code   string `json:"code" jsonschema:"股票代码，如 sh600519"`
	Period string `json:"period,omitempty" jsonschema:"K线周期: 1m(分时), 5d(5日走势), 1d(日线), 1w(周线), 1mo(月线)，默认1d"`
	Days   int    `json:"days,omitzero" jsonschema:"获取天数，默认30；做年化/分位/回撤等统计请传 >=250"`
}

// GetKLineOutput K线数据输出
type GetKLineOutput struct {
	Data string `json:"data" jsonschema:"K线数据"`
}

// createKLineTool 创建K线数据工具
func (r *Registry) createKLineTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input GetKLineInput) (GetKLineOutput, error) {
		fmt.Printf("[Tool:get_kline_data] 调用开始, code=%s, period=%s, days=%d\n", input.Code, input.Period, input.Days)

		if input.Code == "" {
			fmt.Println("[Tool:get_kline_data] 错误: 未提供股票代码")
			return GetKLineOutput{Data: "请提供股票代码"}, nil
		}

		period := input.Period
		if period == "" {
			period = "1d"
		}
		days := input.Days
		if days == 0 {
			days = 30
		}

		klines, err := r.marketService.GetKLineData(input.Code, period, days)
		if err != nil {
			fmt.Printf("[Tool:get_kline_data] 错误: %v\n", err)
			return GetKLineOutput{}, err
		}
		n := len(klines)
		if n == 0 {
			return GetKLineOutput{Data: "无K线数据"}, nil
		}

		var sb strings.Builder

		// 统计摘要：让量化/技术能基于完整样本算年化/分位/回撤，而不是只看最近10根
		if n >= 20 {
			closes := make([]float64, n)
			hi, lo := klines[0].Close, klines[0].Close
			for i, k := range klines {
				closes[i] = k.Close
				if k.High > hi {
					hi = k.High
				}
				if k.Low > 0 && k.Low < lo {
					lo = k.Low
				}
			}
			cur := closes[n-1]
			// 当前收盘在样本中的分位
			below := 0
			for _, c := range closes {
				if c <= cur {
					below++
				}
			}
			pctile := float64(below) * 100 / float64(n)
			ret := func(k int) string {
				if n > k && closes[n-1-k] > 0 {
					return fmt.Sprintf("%.1f%%", (cur/closes[n-1-k]-1)*100)
				}
				return "—"
			}
			// 收盘价最大回撤
			peak, maxDD := closes[0], 0.0
			for _, c := range closes {
				if c > peak {
					peak = c
				}
				if peak > 0 {
					if dd := (c/peak - 1) * 100; dd < maxDD {
						maxDD = dd
					}
				}
			}
			// 日波动率(年化，近似)
			vol := ""
			if n >= 20 {
				var rs []float64
				for i := 1; i < n; i++ {
					if closes[i-1] > 0 {
						rs = append(rs, closes[i]/closes[i-1]-1)
					}
				}
				if len(rs) > 1 {
					var mean float64
					for _, x := range rs {
						mean += x
					}
					mean /= float64(len(rs))
					var v float64
					for _, x := range rs {
						v += (x - mean) * (x - mean)
					}
					std := math.Sqrt(v / float64(len(rs)-1))
					vol = fmt.Sprintf(" 年化波动%.1f%%", std*math.Sqrt(244)*100)
				}
			}
			fmt.Fprintf(&sb, "【统计摘要】%s~%s 共%d根 | 现价%.2f 区间[%.2f,%.2f] 当前分位%.0f%%%s\n",
				klines[0].Time, klines[n-1].Time, n, cur, lo, hi, pctile, vol)
			fmt.Fprintf(&sb, "区间涨跌%s | 近20日%s 近60日%s 近120日%s | 最大回撤%.1f%%\n",
				ret(n-1), ret(20), ret(60), ret(120), maxDD)
		}

		// 最近明细（最多30根）
		start := 0
		if n > 30 {
			start = n - 30
		}
		sb.WriteString("【最近K线】\n")
		for _, k := range klines[start:] {
			fmt.Fprintf(&sb, "%s: 开%.2f 高%.2f 低%.2f 收%.2f 量%d\n",
				k.Time, k.Open, k.High, k.Low, k.Close, k.Volume)
		}

		fmt.Printf("[Tool:get_kline_data] 调用完成, 返回%d条数据\n", n)
		return GetKLineOutput{Data: sb.String()}, nil
	}

	return functiontool.New(functiontool.Config{
		Name:        "get_kline_data",
		Description: "获取股票K线数据(含统计摘要:区间分位/动量/最大回撤/年化波动)，支持分时、5日走势、日线、周线、月线。做年化/分位/回撤统计请传 days>=250",
	}, handler)
}
