//go:build !windows && !darwin && !linux

package discovery

// setReusePort 在不支持 SO_REUSEPORT 的平台上退化为「只开 SO_REUSEADDR」。
//
// 后果是同机多实例时广播可能只被其中一个收到——这只影响开发调试，
// 不影响单实例的正常使用，所以不值得为它写平台特定的 syscall。
func setReusePort(fd uintptr) error { return nil }
