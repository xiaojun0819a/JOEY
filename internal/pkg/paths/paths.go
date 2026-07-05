package paths

import (
	"os"
	"path/filepath"
)

// GetDataDir 获取应用数据目录。
// 优先读环境变量 JCP_DATA_DIR(headless/NAS 部署用它把数据落到 /volume1)。
func GetDataDir() string {
	if d := os.Getenv("JCP_DATA_DIR"); d != "" {
		return d
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil || userConfigDir == "" {
		return filepath.Join(".", "data")
	}
	return filepath.Join(userConfigDir, "jcp")
}

// GetCacheDir 获取缓存目录
func GetCacheDir() string {
	return filepath.Join(GetDataDir(), "cache")
}

// EnsureCacheDir 确保缓存目录存在并返回路径
func EnsureCacheDir(subDir string) string {
	dir := filepath.Join(GetCacheDir(), subDir)
	os.MkdirAll(dir, 0755)
	return dir
}
