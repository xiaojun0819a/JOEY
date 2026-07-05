package tools

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ChipDistInput 筹码分布输入
type ChipDistInput struct {
	Code string `json:"code" jsonschema:"股票代码，如 sh600519"`
	Days int    `json:"days,omitzero" jsonschema:"回溯交易日，默认120(约半年)，用于估算筹码成本分布"`
}

// GetChipDistOutput 筹码分布输出
type GetChipDistOutput struct {
	Data string `json:"data"`
}

// createChipDistTool 由K线(收盘价+成交量)估算筹码分布：平均成本/获利比例/套牢比例/集中度/主要套牢区。
// 东财 cyq 真实筹码接口在本环境不可达，这里用"成交量按价格堆积"的标准近似法计算。
func (r *Registry) createChipDistTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input ChipDistInput) (GetChipDistOutput, error) {
		if input.Code == "" {
			return GetChipDistOutput{Data: "请提供股票代码"}, nil
		}
		if r.marketService == nil {
			return GetChipDistOutput{Data: "Market 服务未初始化"}, nil
		}
		days := input.Days
		if days <= 0 {
			days = 120
		}
		klines, err := r.marketService.GetKLineData(input.Code, "1d", days)
		if err != nil {
			return GetChipDistOutput{}, err
		}
		n := len(klines)
		if n < 20 {
			return GetChipDistOutput{Data: "K线样本不足，无法估算筹码分布"}, nil
		}

		type pv struct{ price, vol float64 }
		pts := make([]pv, 0, n)
		var totalVol, costSum float64
		cur := klines[n-1].Close
		for _, k := range klines {
			tp := (k.High + k.Low + k.Close) / 3 // 典型价作为该日筹码成本
			if tp <= 0 {
				tp = k.Close
			}
			v := float64(k.Volume)
			if v <= 0 {
				continue
			}
			pts = append(pts, pv{tp, v})
			totalVol += v
			costSum += tp * v
		}
		if totalVol <= 0 {
			return GetChipDistOutput{Data: "成交量数据缺失，无法估算筹码"}, nil
		}

		avgCost := costSum / totalVol
		var profitVol, trappedVol float64
		for _, p := range pts {
			if p.price <= cur {
				profitVol += p.vol
			} else {
				trappedVol += p.vol
			}
		}
		profitPct := profitVol / totalVol * 100
		trappedPct := trappedVol / totalVol * 100

		// 集中度：按价格排序后，取覆盖中部 X% 成交量的价格区间宽度 / 中值
		sort.Slice(pts, func(i, j int) bool { return pts[i].price < pts[j].price })
		band := func(frac float64) (lo, hi, conc float64) {
			lower := totalVol * (1 - frac) / 2
			upper := totalVol * (1 + frac) / 2
			var cum float64
			lo, hi = pts[0].price, pts[len(pts)-1].price
			gotLo := false
			for _, p := range pts {
				cum += p.vol
				if !gotLo && cum >= lower {
					lo = p.price
					gotLo = true
				}
				if cum >= upper {
					hi = p.price
					break
				}
			}
			if lo+hi > 0 {
				conc = (hi - lo) / (hi + lo) * 100
			}
			return
		}
		lo90, hi90, conc90 := band(0.90)
		lo70, hi70, conc70 := band(0.70)

		// 主要套牢区：现价上方、成交量最密集的价格档(按 ~3% 价格分箱)
		bin := cur * 0.03
		if bin <= 0 {
			bin = 0.5
		}
		buckets := map[int]float64{}
		for _, p := range pts {
			if p.price > cur {
				buckets[int(p.price/bin)] += p.vol
			}
		}
		mainTrap := ""
		if len(buckets) > 0 {
			bestKey, bestVol := 0, 0.0
			for k, v := range buckets {
				if v > bestVol {
					bestVol, bestKey = v, k
				}
			}
			lo := float64(bestKey) * bin
			mainTrap = fmt.Sprintf("%.2f~%.2f（占比%.0f%%）", lo, lo+bin, bestVol/totalVol*100)
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "【筹码分布·近%d日估算（成交量按价格堆积法）】\n", n)
		fmt.Fprintf(&sb, "现价 %.2f | 平均成本 %.2f（现价%s均本%.1f%%）\n",
			cur, avgCost, ternary(cur >= avgCost, "高于", "低于"), (cur/avgCost-1)*100)
		fmt.Fprintf(&sb, "获利比例 %.0f%% | 套牢比例 %.0f%%\n", profitPct, trappedPct)
		fmt.Fprintf(&sb, "90%%成本区间 %.2f~%.2f（集中度%.1f%%） | 70%%成本区间 %.2f~%.2f（集中度%.1f%%）\n",
			lo90, hi90, conc90, lo70, hi70, conc70)
		if mainTrap != "" {
			fmt.Fprintf(&sb, "主要套牢区(现价上方) %s\n", mainTrap)
		} else {
			sb.WriteString("现价上方基本无套牢筹码（多数获利）\n")
		}
		sb.WriteString("注：集中度越小=筹码越集中；本结果为K线估算，非交易所逐笔筹码。")
		return GetChipDistOutput{Data: sb.String()}, nil
	}

	return functiontool.New(functiontool.Config{
		Name:        "get_chip_distribution",
		Description: "估算个股筹码分布：平均成本、获利/套牢比例、90%/70%成本区间与集中度、主要套牢区。用于判断套牢盘压力与筹码集中度。",
	}, handler)
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
