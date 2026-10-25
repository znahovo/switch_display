package configapp

import (
	"os"
	"time"
)

// writeFile / readFile / statModTime 是测试里的小工具，避免到处 import os。
func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func statModTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
