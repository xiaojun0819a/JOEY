package services

// 数据备份服务:每日 03:30 自动备份可写库与关键文件,防库损坏/误删。
//   - 每日:history/paper/journal/push/audit.db 用 VACUUM INTO 在线备份(WAL 下一致性安全),
//     config.json + users/ + memories/ 打 tar.gz。保留最近 7 天。
//   - 每周日:intraday.db(自采分时,不可重建)单独一份。保留最近 4 份。
//   - archive.db 不备份:9.4G 只读历史档案,可由源 CSV 重建。
// 目录:<dataDir>/backups/daily/<日期>/ 与 backups/weekly/<日期>/。
// 仅权威后端(NAS headless / 本地全量模式)运行;桌面瘦身模式不启动。

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type BackupResult struct {
	At         string   `json:"at"`
	Dest       string   `json:"dest"`
	Files      []string `json:"files"`
	TotalBytes int64    `json:"totalBytes"`
	DurationMs int64    `json:"durationMs"`
	Warnings   []string `json:"warnings"`
	OK         bool     `json:"ok"`
}

type BackupService struct {
	dataDir string
	mu      sync.Mutex
	last    *BackupResult
	lastDay string // 最近一次自动备份的日期,防重复触发
	running bool   // 防重入:上一轮未结束不再起新轮
}

func NewBackupService(dataDir string) *BackupService {
	return &BackupService{dataDir: dataDir}
}

// Start 每分钟检查一次,过 03:30 且当天未备份则执行(周日附带 intraday)。
func (s *BackupService) Start() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			day := now.Format("2006-01-02")
			if now.Hour() < 3 || (now.Hour() == 3 && now.Minute() < 30) {
				continue
			}
			s.mu.Lock()
			done := s.lastDay == day
			s.mu.Unlock()
			if done {
				continue
			}
			res := s.RunBackup(now.Weekday() == time.Sunday)
			s.mu.Lock()
			s.lastDay = day
			s.mu.Unlock()
			if res.OK {
				log.Info("自动备份完成: %s (%.1fMB, %dms)", res.Dest, float64(res.TotalBytes)/1e6, res.DurationMs)
			} else {
				log.Warn("自动备份有告警: %v", res.Warnings)
			}
		}
	}()
}

func (s *BackupService) Status() *BackupResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// RunBackup 立即执行一次备份。withIntraday 时额外备份 intraday.db 到 weekly。
func (s *BackupService) RunBackup(withIntraday bool) BackupResult {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return BackupResult{At: time.Now().Format("2006-01-02 15:04:05"), OK: false, Warnings: []string{"上一轮备份仍在进行,跳过"}}
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	start := time.Now()
	res := BackupResult{At: start.Format("2006-01-02 15:04:05"), OK: true}
	day := start.Format("2006-01-02")
	dest := filepath.Join(s.dataDir, "backups", "daily", day)
	res.Dest = dest
	if err := os.MkdirAll(dest, 0o755); err != nil {
		res.OK = false
		res.Warnings = append(res.Warnings, "建目录失败: "+err.Error())
		return res
	}

	warn := func(f string, a ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(f, a...))
	}

	// 1) 可写库:WAL 检查点后拷贝主文件,再对副本做完整性校验。
	// (不用 VACUUM INTO:modernc 驱动对大库会卡死且挂读事务阻塞 checkpoint,2026-07-09 实测)
	for _, name := range []string{"history.db", "paper.db", "journal.db", "push.db", "audit.db"} {
		src := filepath.Join(s.dataDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := backupSQLite(src, filepath.Join(dest, name)); err != nil {
			warn("%s 备份失败: %v", name, err)
			res.OK = false
			continue
		}
		res.Files = append(res.Files, name)
	}

	// 2) 关键文件:config.json + users/ + memories/ 打 tar.gz(只打存在的)
	var tarItems []string
	for _, item := range []string{"config.json", "users", "memories", "remote_sessions.json"} {
		if _, err := os.Stat(filepath.Join(s.dataDir, item)); err == nil {
			tarItems = append(tarItems, item)
		}
	}
	if len(tarItems) > 0 {
		tarPath := filepath.Join(dest, "files.tgz")
		args := append([]string{"-czf", tarPath, "-C", s.dataDir}, tarItems...)
		if out, err := exec.Command("tar", args...).CombinedOutput(); err != nil {
			warn("files.tgz 失败: %v %s", err, strings.TrimSpace(string(out)))
			res.OK = false
		} else {
			res.Files = append(res.Files, "files.tgz")
		}
	}

	// 3) 周备:intraday.db(大,自采不可重建)
	if withIntraday {
		src := filepath.Join(s.dataDir, "intraday.db")
		if _, err := os.Stat(src); err == nil {
			wdest := filepath.Join(s.dataDir, "backups", "weekly", day)
			if err := os.MkdirAll(wdest, 0o755); err == nil {
				if err := backupSQLite(src, filepath.Join(wdest, "intraday.db")); err != nil {
					warn("intraday.db 周备失败: %v", err)
				} else {
					res.Files = append(res.Files, "weekly/intraday.db")
				}
			}
		}
	}

	// 4) 轮转:日备留 7,周备留 4
	pruneBackups(filepath.Join(s.dataDir, "backups", "daily"), 7)
	pruneBackups(filepath.Join(s.dataDir, "backups", "weekly"), 4)

	// 统计体积
	_ = filepath.Walk(dest, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			res.TotalBytes += info.Size()
		}
		return nil
	})
	res.DurationMs = time.Since(start).Milliseconds()

	s.mu.Lock()
	s.last = &res
	s.mu.Unlock()
	return res
}

// backupSQLite 备份一个 sqlite 库:先 WAL 检查点把 -wal 合并进主文件,
// 再拷贝主文件(备份窗口在凌晨无写入时段,拷贝即一致快照),最后对副本 quick_check 校验。
func backupSQLite(src, dst string) error {
	_ = os.Remove(dst)
	_ = os.Remove(dst + "-journal")
	if db, err := sql.Open("sqlite", src); err == nil {
		_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		_ = db.Close()
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	// 副本完整性校验
	chk, err := sql.Open("sqlite", dst)
	if err != nil {
		return err
	}
	defer chk.Close()
	var status string
	if err = chk.QueryRow("PRAGMA quick_check").Scan(&status); err != nil {
		return err
	}
	if !strings.EqualFold(status, "ok") {
		return fmt.Errorf("副本校验未通过: %s", status)
	}
	return nil
}

func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var days []string
	for _, e := range entries {
		if e.IsDir() {
			days = append(days, e.Name())
		}
	}
	if len(days) <= keep {
		return
	}
	sort.Strings(days)
	for _, d := range days[:len(days)-keep] {
		_ = os.RemoveAll(filepath.Join(dir, d))
	}
}
