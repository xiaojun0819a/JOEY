package main

// 访客操作审计：headless 版把访客的每次 RPC 调用记入 dataDir/audit.db，
// 主人通过 /admin 管理页或 GetAuditLogs(admin 专属)按用户查看选股与操作记录。
// 桌面版编译进来但从不写入(recordAudit 只被 headless 的 RPC 分发器调用)。

import (
	"database/sql"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
)

// AuditEntry 一条操作记录
type AuditEntry struct {
	ID       int64  `json:"id"`
	Time     string `json:"time"` // RFC3339
	Username string `json:"username"`
	Method   string `json:"method"`
	Args     string `json:"args"` // JSON 参数(截断,密码类已抹除)
	IP       string `json:"ip"`
}

// AuditUserSummary 用户维度汇总
type AuditUserSummary struct {
	Username string `json:"username"`
	Count    int64  `json:"count"`
	LastTime string `json:"lastTime"`
}

var (
	auditOnce sync.Once
	auditDB   *sql.DB
	auditCh   chan AuditEntry
)

func auditInit() *sql.DB {
	auditOnce.Do(func() {
		dbPath := filepath.Join(paths.GetDataDir(), "audit.db")
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			log.Error("审计库打开失败: %v", err)
			return
		}
		db.SetMaxOpenConns(1) // 单写入协程 + 偶发查询,串行即可
		if _, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS audit_log (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				ts TEXT NOT NULL,
				username TEXT NOT NULL,
				method TEXT NOT NULL,
				args TEXT,
				ip TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_audit_user_ts ON audit_log(username, ts);
			CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts);
		`); err != nil {
			log.Error("审计表创建失败: %v", err)
			_ = db.Close()
			return
		}
		auditDB = db
		auditCh = make(chan AuditEntry, 1024)
		go func() {
			for e := range auditCh {
				_, _ = auditDB.Exec(
					"INSERT INTO audit_log(ts, username, method, args, ip) VALUES(?,?,?,?,?)",
					e.Time, e.Username, e.Method, e.Args, e.IP,
				)
			}
		}()
	})
	return auditDB
}

// recordAudit 异步写一条审计记录;队列满则丢弃,绝不阻塞 RPC 路径。
func recordAudit(username, method, args, ip string) {
	if auditInit() == nil {
		return
	}
	if len(args) > 600 {
		args = args[:600] + "…"
	}
	select {
	case auditCh <- AuditEntry{Time: time.Now().Format(time.RFC3339), Username: username, Method: method, Args: args, IP: ip}:
	default:
	}
}

// GetAuditLogs 查询操作记录(仅主人令牌可调,headless 对访客拉黑)。
// username/method 为过滤条件(空=不过滤,method 支持子串匹配),按时间倒序。
func (a *App) GetAuditLogs(username, method string, limit, offset int) ([]AuditEntry, error) {
	db := auditInit()
	if db == nil {
		return []AuditEntry{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	q := "SELECT id, ts, username, method, args, ip FROM audit_log WHERE 1=1"
	argsQ := []any{}
	if username != "" {
		q += " AND username = ?"
		argsQ = append(argsQ, username)
	}
	if method != "" {
		q += " AND method LIKE ?"
		argsQ = append(argsQ, "%"+method+"%")
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	argsQ = append(argsQ, limit, offset)
	rows, err := db.Query(q, argsQ...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Time, &e.Username, &e.Method, &e.Args, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetAuditUsers 用户维度汇总(仅主人令牌可调):每个用户的操作数与最近活跃时间。
func (a *App) GetAuditUsers() ([]AuditUserSummary, error) {
	db := auditInit()
	if db == nil {
		return []AuditUserSummary{}, nil
	}
	rows, err := db.Query("SELECT username, COUNT(*), MAX(ts) FROM audit_log GROUP BY username ORDER BY MAX(ts) DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditUserSummary{}
	for rows.Next() {
		var s AuditUserSummary
		if err := rows.Scan(&s.Username, &s.Count, &s.LastTime); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
