// Package persistence 把配置存成单个 JSON 文件（ADR-004）。
//
// 单文件 + 原子写 + .bak 的理由：配置很小（几十个信号源），
// 用数据库或分片文件只会增加损坏面；原子写保证断电不会留下半个 JSON。
package persistence

import (
	"os"
	"path/filepath"
)

// AppDirName 是配置目录名，与 build/config.yml 的 productIdentifier 末段一致。
const AppDirName = "switch-display"

// ConfigFileName 是配置文件名。
const ConfigFileName = "config.json"

// DefaultPath 返回当前平台的配置路径。
//
//	darwin  ~/Library/Application Support/switch-display/config.json
//	windows %AppData%\switch-display\config.json
//	linux   $XDG_CONFIG_HOME/switch-display/config.json（默认 ~/.config）
//
// 之所以用各平台惯例路径而不是「程序旁边」：macOS 上 app bundle 通常不可写，
// 且用户期望设置跟着用户账号而不是安装位置。
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppDirName, ConfigFileName), nil
}
