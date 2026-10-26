//go:build windows

// Package windows 实现 Windows 上的 DDC/CI 读写与显示器枚举。
//
// 三层职责分明：
//   - port.go 是 display.Port 的实现，负责「找显示器 / 读 / 写」的流程；
//   - win32.go 是 user32/dxva2 的薄封装，只做 DLL 调用；
//   - edid.go 是纯函数式的 EDID 解析。
//
// 实现选择（见 ADR-008）：
//   - 枚举：EnumDisplayMonitors + GetMonitorInfoW 拿 GDI 名与 HMONITOR，
//     再用 EnumDisplayDevicesW(EDD_GET_DEVICE_INTERFACE_NAME) 拿设备接口路径；
//   - 读写：Dxva2 的 GetPhysicalMonitorsFromHMONITOR + SetVCPFeature /
//     GetVCPFeatureAndVCPFeatureReply（系统自带的 DDC/CI 入口，不自己碰 I2C）；
//   - 元数据：注册表 Enum\<实例>\Device Parameters 下的 EDID，不占用 DDC 总线。
//
// 不用 cgo：architecture.md R6 只允许 darwin 包 import "C"，
// 而 Windows 这几个 DLL 都有稳定导出，x/sys/windows 的 LazyDLL 足够。
package windows

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// Port 是 display.Port 在 Windows 上的实现。
type Port struct {
	// mu 串行化所有 DDC 操作。Dxva2 的监视器句柄本身不是线程安全的，
	// 且并发读同一台显示器会互相打乱 DDC 事务（I2C 层没有事务隔离），
	// 结果是「偶尔读到垃圾值」——这类问题极难排查，不如串行化。
	mu sync.Mutex
}

// New 构造 Windows DDC 端口。
func New() *Port { return &Port{} }

// 编译期断言：Port 必须实现 display.Port。
var _ display.Port = (*Port)(nil)

// List 枚举当前接到桌面上的显示器。
func (p *Port) List(ctx context.Context) ([]display.Display, error) {
	monitors, err := enumMonitors(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]display.Display, 0, len(monitors))
	// seen 去重：显示模式设为「复制」时同一台显示器会出现两个 HMONITOR /
	// 两个 GDI 名，但对用户来说只有一台。
	seen := make(map[display.DisplayID]struct{}, len(monitors))

	for _, m := range monitors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := seen[m.id]; ok {
			continue
		}
		seen[m.id] = struct{}{}

		d := display.Display{ID: m.id}

		// 名字优先用 EDID 里的描述符（"P25A2G"），那才是用户在显示器
		// OSD 里看到的名字；拿不到才退回 PnP 组合名（"XMI27B1"）。
		if data, ok := readEDID(m.id); ok {
			d.EDID = hex.EncodeToString(data)
			if info, ok := parseEDID(data); ok {
				d.Name = info.Name
				d.Vendor = info.Vendor
				d.Model = info.Model
				d.Serial = info.Serial
			}
		}
		if d.Name == "" {
			d.Name = fallbackName(string(m.id))
		}

		// Builtin 供上层排序（外接优先）。Windows 没有可靠的「内屏」标记，
		// 但内屏与虚拟显示器有个共同点：都不提供 DDC 物理通道。用这个判据
		// 顺带把虚拟显示器排到后面，正是本项目想要的效果。
		var count uint32
		d.Builtin = !physicalMonitorCount(m.hMon, &count)

		out = append(out, d)
	}
	return out, nil
}

// ReadVCP 读取一个 VCP 代码的当前值。
//
// 读回是 best-effort：通信失败、显示器不支持、没有 DDC 通道，都返回
// Supported=false 与 nil error——上层据此走 applied_unconfirmed，而不是
// 报错中断切换流程（与 darwin 实现保持一致）。
func (p *Port) ReadVCP(ctx context.Context, id display.DisplayID, vcp display.VCPCode, alt bool) (display.ReadBack, error) {
	if err := ctx.Err(); err != nil {
		return display.ReadBack{}, err
	}
	if alt {
		// Dxva2 只暴露 VCP 层，没有「换 I2C 从地址」的能力：0xF4 这类需要
		// 切到 0x50 的 LG 私有通道在 Windows 上做不到（见 ADR-009）。
		return display.ReadBack{Supported: false}, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	handle, release, err := p.open(ctx, id)
	if err != nil {
		if errors.Is(err, display.ErrDisplayNotFound) {
			// 显示器不在了是调用方的错，不是「读不到」，必须如实上报——
			// 否则用户拔掉显示器后仍会看到打勾停留在旧源上。
			return display.ReadBack{}, err
		}
		return display.ReadBack{Supported: false}, nil
	}
	defer release()

	// result 用 uint32 接：Dxva2 给的是 DWORD，高位对 VCP 无意义。
	current, maximum, err := getVCP(handle, byte(vcp))
	if err != nil {
		return display.ReadBack{Supported: false}, nil
	}
	return display.ReadBack{
		Supported: true,
		Current:   uint16(current),
		Maximum:   uint16(maximum),
		// Raw 留空：Dxva2 不回传原始 DDC 帧，硬凑一个只会误导诊断。
	}, nil
}

// WriteVCP 写入一个 VCP 值。
func (p *Port) WriteVCP(ctx context.Context, id display.DisplayID, v display.InputValue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.Alt {
		// 同 ReadVCP：Windows 上无法走 0x50 从地址的私有通道。
		// 返回 ErrUnsupported 而不是 ErrWriteFailed，是因为「这台机器做不到」
		// 与「这次写失败」对用户是两件事，前者不该被无意义地重试。
		return fmt.Errorf("%w: alternate DDC channel is not available on Windows", display.ErrUnsupported)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	handle, release, err := p.open(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	if err := setVCP(handle, byte(v.VCP), v.Value); err != nil {
		return fmt.Errorf("%w: %v", display.ErrWriteFailed, err)
	}
	return nil
}

// open 找到 DisplayID 对应的物理监视器并打开 DDC 通道。
//
// 返回的 release 必须被调用。句柄本身可能是 0（本机 DISPLAY1 就是 0），
// 所以失败只能靠 error 判断，不能看句柄值。
func (p *Port) open(ctx context.Context, id display.DisplayID) (uintptr, func(), error) {
	monitors, err := enumMonitors(ctx)
	if err != nil {
		return 0, nil, err
	}
	for _, m := range monitors {
		if m.id != id {
			continue
		}
		return openPhysical(m.hMon)
	}
	return 0, nil, fmt.Errorf("%w: %s", display.ErrDisplayNotFound, id)
}
