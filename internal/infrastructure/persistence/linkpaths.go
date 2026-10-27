package persistence

import (
	"os"
	"path/filepath"
)

// LinkFileName 是设备身份与已配对设备的文件名。
//
// 与 config.json 分开的理由见 docs/deskflow-integration.md §3.3：
// 这里存的是「运维态」（本机 ID、已配对列表），丢了可以重新生成/重新配对，
// 不必让它参与 config.json 的版本迁移。
const LinkFileName = "link.json"

// DeskflowDirName 是 Deskflow 相关文件所在的子目录名。
//
// 放在 switch-display 目录下的原因：不污染用户自己安装的 Deskflow GUI
// （它默认用 ~/Library/Deskflow 或 ~/.config/Deskflow）。
const DeskflowDirName = "deskflow"

// AppDir 返回 switch-display 的配置目录。
func AppDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppDirName), nil
}

// DefaultLinkPath 返回 link.json 的默认路径。
func DefaultLinkPath() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LinkFileName), nil
}

// DefaultDeskflowDir 返回我们托管 Deskflow 配置文件的目录。
//
// 目录内布局：
//
//	deskflow.conf          core 的 QSettings INI
//	deskflow-server.conf   server 模式的布局文件
//	tls/deskflow.pem       我们生成的证书（证书+私钥同文件）
//	tls/trusted-clients    server 端信任的客机指纹
//	tls/trusted-servers    client 端信任的主机指纹
func DefaultDeskflowDir() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DeskflowDirName), nil
}
