package link

import "errors"

// 领域校验错误。表现层在 errors.go 里把它们映射成稳定的错误码。
var (
	// ErrInvalidDeviceID 表示设备 ID 不是 32 位小写 hex。
	ErrInvalidDeviceID = errors.New("invalid device id")
	// ErrInvalidName 表示设备名为空或超长。
	ErrInvalidName = errors.New("invalid device name")
	// ErrInvalidRole 表示角色不是 idle/server/client。
	ErrInvalidRole = errors.New("invalid role")
	// ErrInvalidFingerprint 表示指纹不是 v2:sha256:<hex> 格式。
	ErrInvalidFingerprint = errors.New("invalid fingerprint")
	// ErrInvalidPort 表示端口越界。
	ErrInvalidPort = errors.New("invalid port")
	// ErrInvalidMessage 表示信令报文结构非法。
	ErrInvalidMessage = errors.New("invalid message")
)
