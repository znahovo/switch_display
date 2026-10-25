package display

import "errors"

var (
	// ErrDisplayNotFound 表示给定的 DisplayID 不在当前枚举结果里（比如被拔掉了）。
	ErrDisplayNotFound = errors.New("display not found")
	// ErrUnsupported 表示当前平台/当前显示器不支持该操作。
	ErrUnsupported = errors.New("operation not supported")
	// ErrWriteFailed 表示写入 DDC/CI 失败（重试后仍失败）。
	ErrWriteFailed = errors.New("ddc write failed")
)
