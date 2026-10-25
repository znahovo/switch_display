package display

import "context"

// Port 是 DDC/CI 能力端口（由 infrastructure 实现）。
//
// 之所以定义在领域层：领域服务需要「枚举显示器 / 读 / 写」这些能力，
// 但不应知道它们是 IOKit、Dxva2 还是 i2c-dev（architecture.md §5）。
type Port interface {
	// List 枚举当前可用的显示器。
	List(ctx context.Context) ([]Display, error)
	// ReadVCP 读取某个 VCP 代码的当前值。显示器不支持时返回 Supported=false，
	// 而不是 error——「不支持」是正常状态，不是故障。
	ReadVCP(ctx context.Context, id DisplayID, vcp VCPCode, alt bool) (ReadBack, error)
	// WriteVCP 把某个 VCP 代码设为目标值。
	WriteVCP(ctx context.Context, id DisplayID, v InputValue) error
}
