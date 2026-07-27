package main

// 情报库文件上传(2026-07-27,第二大脑多模态第一步)。
//
// 路径:前端读文件 → base64 → 这里解析成纯文字 → 交给既有的 AddIntelNote。
// 之所以让 Go 解析而不是浏览器解析:docx 是 zip 二进制,浏览器侧要额外引库;
// 而 Go 这边 archive/zip 是标准库,零依赖。
//
// 本轮支持 txt/md/csv/docx(零依赖)。**PDF 故意先不做**——需要引第三方解析库,
// 而且扫描版 PDF 还得 OCR,不是加个分支就完事,等这条链路跑顺了再单独评估。
// 图片/视频同理,见 [[第二大脑多模态]] 的分步计划。
//
// 复用既有逻辑的好处:AddIntelNote 里已经做了「链接抓正文」和「全市场股票名/代码自动识别」,
// 所以一份研报传进来,里面点名的锐捷网络/紫光股份这些会自动关联上,不用另写一套。

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

// docx 正文:<w:t> 里是文字,<w:p> 是段落。去标签时把段落换成换行,否则整篇糊成一行。
var (
	docxParaRe = regexp.MustCompile(`</w:p>`)
	docxTagRe  = regexp.MustCompile(`<[^>]+>`)
)

func parseDocx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("不是有效的 docx(zip 打不开):%w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		s := docxParaRe.ReplaceAllString(string(raw), "\n")
		s = docxTagRe.ReplaceAllString(s, "")
		s = strings.ReplaceAll(s, "&amp;", "&")
		s = strings.ReplaceAll(s, "&lt;", "<")
		s = strings.ReplaceAll(s, "&gt;", ">")
		s = strings.ReplaceAll(s, "&quot;", "\"")
		return s, nil
	}
	return "", fmt.Errorf("docx 里没有 word/document.xml")
}

// IntelUploadResult 上传结果:除了笔记本身,把「解析出多少字、认出哪些票」如实回给界面,
// 让用户一眼看出解析对不对,而不是入库完就没下文。
type IntelUploadResult struct {
	Note      *IntelNote `json:"note"`
	FileName  string     `json:"fileName"`
	TextLen   int        `json:"textLen"`
	Preview   string     `json:"preview"`
	Warning   string     `json:"warning"`
	CodeCount int        `json:"codeCount"`
}

// AddIntelNoteFromUpload 前端把文件读成 base64 传进来,这里解析成文字后入库。
func (a *App) AddIntelNoteFromUpload(fileName string, dataB64 string, codes []string) (*IntelUploadResult, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return nil, fmt.Errorf("文件名为空")
	}
	// 前端可能传 dataURL(data:xxx;base64,....),去掉前缀
	if i := strings.Index(dataB64, ";base64,"); i >= 0 {
		dataB64 = dataB64[i+len(";base64,"):]
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(dataB64))
	if err != nil {
		return nil, fmt.Errorf("文件内容解码失败:%w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("文件是空的")
	}
	if len(data) > 20<<20 {
		return nil, fmt.Errorf("文件超过 20MB,先截一段文字粘贴进来")
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	var text string
	warning := ""
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".json", ".log":
		text = string(data)
	case ".docx":
		text, err = parseDocx(data)
		if err != nil {
			return nil, err
		}
	case ".doc":
		// 老式 .doc 是二进制复合文档,不是 zip;但本项目生成的"投研报告.doc"其实是 HTML 换了后缀,
		// 所以先按 HTML 去标签试一把,真是老 doc 会得到乱码,提示用户另存为 docx。
		s := docxTagRe.ReplaceAllString(string(data), " ")
		if strings.Count(s, "�") > len(s)/20 {
			return nil, fmt.Errorf("这是老式 .doc 二进制格式,请另存为 .docx 或 .txt 再传")
		}
		text = s
		warning = "老式 .doc 按 HTML 兜底解析,若内容错乱请另存为 .docx"
	case ".pdf":
		return nil, fmt.Errorf("PDF 暂未支持(需要额外解析库,扫描件还得 OCR)。先复制文字粘贴,或另存为 .txt/.docx")
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return nil, fmt.Errorf("图片暂未支持(要先确认 AI 网关是否具备视觉能力)。这一步只做文本类文件")
	case ".mp4", ".mov", ".mkv", ".avi", ".m4a", ".mp3", ".wav":
		return nil, fmt.Errorf("音视频暂未支持(需要语音转文字能力,项目里还没有)。有字幕/文案的话直接粘文字")
	default:
		// 未知后缀:如果整体是可读文本就按文本处理,否则拒绝
		s := string(data)
		if strings.Count(s, "\x00") > 0 {
			return nil, fmt.Errorf("无法识别的二进制文件(%s),支持 txt/md/csv/docx", ext)
		}
		text = s
		warning = fmt.Sprintf("未知后缀 %s,按纯文本处理", ext)
	}

	text = strings.TrimSpace(text)
	// 压掉 docx 解析后常见的连续空行
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	if text == "" {
		return nil, fmt.Errorf("从 %s 里没解析出文字", fileName)
	}

	note, err := a.AddIntelNote(text, codes, "file:"+fileName)
	if err != nil {
		return nil, err
	}
	preview := text
	if len([]rune(preview)) > 200 {
		preview = string([]rune(preview)[:200]) + "…"
	}
	return &IntelUploadResult{
		Note:      note,
		FileName:  fileName,
		TextLen:   len([]rune(text)),
		Preview:   preview,
		Warning:   warning,
		CodeCount: len(note.Codes),
	}, nil
}
