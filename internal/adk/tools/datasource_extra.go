package tools

import (
	"fmt"
	"strings"

	"github.com/run-bigpig/jcp/internal/services"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ---- 股吧舆情 ----

// GubaSentimentInput 股吧舆情输入
type GubaSentimentInput struct {
	Code  string `json:"code" jsonschema:"股票代码，如 sh600519 或 600519"`
	Limit int    `json:"limit,omitzero" jsonschema:"返回热帖条数，默认10，最大30"`
}

// GubaSentimentOutput 股吧舆情输出
type GubaSentimentOutput struct {
	Data string `json:"data"`
}

// createGubaSentimentTool 东财股吧个股热帖:散户真实情绪、题材发酵线索。标题原文返回,情绪判断交给模型。
func (r *Registry) createGubaSentimentTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input GubaSentimentInput) (GubaSentimentOutput, error) {
		if strings.TrimSpace(input.Code) == "" {
			return GubaSentimentOutput{Data: "请提供股票代码"}, nil
		}
		sum, err := services.DefaultGubaService().GetGubaSummary(input.Code, input.Limit)
		if err != nil {
			return GubaSentimentOutput{Data: "股吧数据获取失败: " + err.Error()}, nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "【%s(%s)股吧热帖 · 抓取于%s】\n", sum.BarName, sum.Code, sum.FetchedAt)
		fmt.Fprintf(&sb, "说明:按股吧首页排序取前%d条,标题为散户原文,阅读/评论数反映关注度。\n", len(sum.Posts))
		for i, p := range sum.Posts {
			fmt.Fprintf(&sb, "%d. %s (阅读%d/评论%d, %s)\n", i+1, p.Title, p.Clicks, p.Comments, p.Time)
		}
		return GubaSentimentOutput{Data: sb.String()}, nil
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_guba_sentiment",
		Description: "获取东方财富股吧个股热帖列表(标题/阅读/评论数)，用于判断散户情绪与题材发酵迹象",
	}, handler)
}

// ---- 市场情绪面 ----

// MarketMoodInput 市场情绪面输入(无必填参数)
type MarketMoodInput struct {
	Placeholder string `json:"placeholder,omitzero" jsonschema:"留空即可"`
}

// MarketMoodOutput 市场情绪面输出
type MarketMoodOutput struct {
	Data string `json:"data"`
}

// createMarketMoodTool 全市场情绪面:涨跌家数分布 + 两融余额5日趋势 + 行业板块涨跌榜。
func (r *Registry) createMarketMoodTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, _ MarketMoodInput) (MarketMoodOutput, error) {
		mood, err := services.DefaultMarketMoodService().GetMarketMood()
		if err != nil {
			return MarketMoodOutput{Data: "市场情绪面获取失败: " + err.Error()}, nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "【市场情绪面 · %s】\n", mood.FetchedAt)
		if mood.UpCount+mood.DownCount > 0 {
			fmt.Fprintf(&sb, "涨跌家数(%s): 上涨%d / 下跌%d / 平盘%d;涨≥7%%共%d家,跌≤-7%%共%d家\n",
				mood.Date, mood.UpCount, mood.DownCount, mood.FlatCount, mood.StrongCount, mood.WeakCount)
		}
		if n := len(mood.MarginTrend); n > 0 {
			first, last := mood.MarginTrend[0], mood.MarginTrend[n-1]
			delta := (last.RZRQTotal - first.RZRQTotal) / 1e8
			fmt.Fprintf(&sb, "两融余额: 最新(%s) %.0f亿", last.Date, last.RZRQTotal/1e8)
			fmt.Fprintf(&sb, ",较%s %+.0f亿\n", first.Date, delta)
			for _, d := range mood.MarginTrend {
				fmt.Fprintf(&sb, "  %s 两融%.0f亿(融资%.0f亿)\n", d.Date, d.RZRQTotal/1e8, d.RZYE/1e8)
			}
		}
		if len(mood.TopSectors) > 0 {
			sb.WriteString("行业板块领涨: ")
			for i, x := range mood.TopSectors {
				if i > 0 {
					sb.WriteString("、")
				}
				fmt.Fprintf(&sb, "%s%+.2f%%", x.Name, x.ChangePct)
			}
			sb.WriteString("\n行业板块领跌: ")
			for i, x := range mood.BottomSectors {
				if i > 0 {
					sb.WriteString("、")
				}
				fmt.Fprintf(&sb, "%s%+.2f%%", x.Name, x.ChangePct)
			}
			sb.WriteString("\n")
		}
		return MarketMoodOutput{Data: sb.String()}, nil
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_market_mood",
		Description: "获取全市场情绪面快照：涨跌家数分布、沪深两融余额近5日趋势、行业板块领涨领跌榜",
	}, handler)
}

// ---- 巨潮公告(官方源,支持关键词) ----

// CninfoAnnInput 巨潮公告输入
type CninfoAnnInput struct {
	Code      string `json:"code" jsonschema:"股票代码，如 sh600519 或 600519"`
	SearchKey string `json:"searchKey,omitzero" jsonschema:"公告关键词过滤，如 问询函/减持/回购/业绩预告，留空返回最新公告"`
	Limit     int    `json:"limit,omitzero" jsonschema:"返回条数，默认10，最大30"`
}

// CninfoAnnOutput 巨潮公告输出
type CninfoAnnOutput struct {
	Data string `json:"data"`
}

// createCninfoAnnTool 巨潮资讯官方公告查询,支持关键词(问询函/减持等监管敏感类是与东财公告源的差异价值)。
func (r *Registry) createCninfoAnnTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input CninfoAnnInput) (CninfoAnnOutput, error) {
		if strings.TrimSpace(input.Code) == "" {
			return CninfoAnnOutput{Data: "请提供股票代码"}, nil
		}
		res, err := services.DefaultCninfoService().QueryAnnouncements(input.Code, input.SearchKey, input.Limit)
		if err != nil {
			return CninfoAnnOutput{Data: "巨潮公告查询失败: " + err.Error()}, nil
		}
		var sb strings.Builder
		if res.SearchKey != "" {
			fmt.Fprintf(&sb, "【巨潮官方公告 · %s · 关键词\"%s\" · 共%d条(展示%d条)】\n", res.Code, res.SearchKey, res.Total, len(res.Announcements))
		} else {
			fmt.Fprintf(&sb, "【巨潮官方公告 · %s · 共%d条(展示最新%d条)】\n", res.Code, res.Total, len(res.Announcements))
		}
		if len(res.Announcements) == 0 {
			sb.WriteString("无匹配公告。\n")
		}
		for i, a := range res.Announcements {
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, a.Time, a.Title)
		}
		return CninfoAnnOutput{Data: sb.String()}, nil
	}
	return functiontool.New(functiontool.Config{
		Name:        "get_cninfo_announcements",
		Description: "查询巨潮资讯(证监会官方披露平台)个股公告，支持关键词过滤如问询函/减持/回购，一手权威信源",
	}, handler)
}
