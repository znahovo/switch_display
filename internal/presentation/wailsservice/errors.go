package wailsservice

import (
	"errors"

	"github.com/znahovo/switch-display/internal/application/linkapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
)

// Error 是返回给前端的结构化错误。
//
// Wails 会把实现了 error 的返回值序列化；带 Code/Message 字段让前端可以
// 按 code 做 i18n，而不是靠匹配英文句子。
type Error struct {
	// Code 是稳定的机器可读标识：invalid_name / duplicate_port / io / unsupported …
	Code string `json:"code"`
	// Op 是失败的操作名，便于日志定位。
	Op string `json:"op"`
	// Message 是英文兜底文案；前端有 i18n 时应优先用自己的翻译。
	Message string `json:"message"`
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e.Op == "" {
		return e.Message
	}
	return e.Op + ": " + e.Message
}

// newError 把任意领域/基础设施错误翻译成前端可用的结构化错误。
//
// 翻译表是刻意写死的：前端只认这些 code，新增错误类型时这里必须一起改，
// 否则前端会退化成显示英文原文。
func newError(err error, op string) *Error {
	if err == nil {
		return nil
	}

	code := "internal"
	msg := err.Error()

	switch {
	case errors.Is(err, source.ErrInvalidName):
		code, msg = "invalid_name", "signal source name is required (max 32 characters)"
	case errors.Is(err, source.ErrInvalidColor):
		code, msg = "invalid_color", "color must be a hex value like #3b82f6"
	case errors.Is(err, source.ErrInvalidDelay):
		code, msg = "invalid_delay", "confirm delay must be between 0 and 10000 ms"
	case errors.Is(err, source.ErrInvalidInput):
		code, msg = "invalid_input", "input source value is not valid"
	case errors.Is(err, source.ErrDuplicateShortcut):
		code, msg = "duplicate_shortcut", "the same shortcut is bound to more than one source"
	case errors.Is(err, source.ErrDuplicatePort):
		code, msg = "duplicate_port", "the same display input is used by more than one source"
	case errors.Is(err, source.ErrDuplicateSourceID):
		code, msg = "duplicate_source", "two sources share the same id"
	case errors.Is(err, display.ErrDisplayNotFound):
		code, msg = "display_not_found", "display is no longer connected"
	case errors.Is(err, display.ErrUnsupported):
		code, msg = "unsupported", "this display does not support the requested DDC command"
	case errors.Is(err, display.ErrWriteFailed):
		code, msg = "write_failed", "the display did not accept the DDC/CI write"
	case errors.Is(err, linkapp.ErrNoCore):
		code, msg = "deskflow_missing", "Deskflow is not installed; install Deskflow to share mouse and keyboard"
	case errors.Is(err, linkapp.ErrPeerNotFound):
		code, msg = "peer_not_found", "the device is no longer visible on the network"
	case errors.Is(err, linkapp.ErrNoIdentity):
		code, msg = "deskflow_cert", "the local Deskflow certificate is unavailable; restart the app to regenerate it"
	case errors.Is(err, linkapp.ErrBusy):
		code, msg = "link_busy", "sharing is already active; stop it first"
	case errors.Is(err, linkapp.ErrNotPaired):
		code, msg = "not_paired", "this device is not paired yet"
	case errors.Is(err, linkapp.ErrInvalidName):
		// 与信号源的 invalid_name 分开：两者长度上限不同（64 vs 32），
		// 共用一个 code 会让前端翻出一条上限对不上的提示。
		code, msg = "invalid_device_name", "device name is required (max 64 characters)"
	}

	return &Error{Code: code, Op: op, Message: msg}
}
