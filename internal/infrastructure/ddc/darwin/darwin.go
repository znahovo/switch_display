// Package darwin 实现 macOS（Apple Silicon）上的 DDC/CI 读写。
//
// 实现方式是把 m1ddc（MIT, waydabber）的核心逻辑移植成同包的 C/ObjC 文件，
// 由 cgo 直接编译进二进制——用户不需要额外安装任何东西（ADR-001）。
//
// 用到的 IOAVService* 与 CoreDisplay_DisplayCreateInfoDictionary 是私有 API，
// 通过 dlsym 动态解析（见 registry.m），符号缺失时降级为「不支持」而不是崩溃。
package darwin

/*
#cgo CFLAGS: -fmodules -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework IOKit -framework CoreGraphics -framework CoreFoundation
#include "registry.h"
#include "i2c.h"
#include <stdlib.h>
#define KERN_SUCCESS 0
*/
import "C"

import (
	"context"
	"fmt"
	"sync"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// Port 是 display.Port 在 macOS 上的实现。
type Port struct {
	mu sync.Mutex // IOAVServiceRef 不是线程安全的，串行化所有 DDC 操作
}

// New 构造 macOS DDC 端口。
func New() *Port { return &Port{} }

// List 枚举在线显示器。
func (p *Port) List(ctx context.Context) ([]display.Display, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records := make([]C.SDDisplayRecord, C.SD_MAX_DISPLAYS)
	n := int(C.sd_list_displays(&records[0], C.int(len(records))))
	if n < 0 {
		return nil, fmt.Errorf("enumerate displays: code %d", n)
	}
	defer C.sd_release_displays(&records[0], C.int(n))

	out := make([]display.Display, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, display.Display{
			ID:      display.DisplayID(C.GoString(&records[i].uuid[0])),
			Name:    C.GoString(&records[i].name[0]),
			Vendor:  fmt.Sprintf("%d", uint32(records[i].vendorNum)),
			Model:   fmt.Sprintf("%d", uint32(records[i].modelNum)),
			Serial:  C.GoString(&records[i].anSerial[0]),
			EDID:    C.GoString(&records[i].edid[0]),
			Builtin: records[i].builtin != 0,
		})
	}
	return out, nil
}

// ReadVCP 读取一个 VCP 代码的当前值。
//
// 显示器不支持读回时返回 Supported=false 与 nil error——「不支持」是正常状态。
func (p *Port) ReadVCP(ctx context.Context, id display.DisplayID, vcp display.VCPCode, alt bool) (display.ReadBack, error) {
	if err := ctx.Err(); err != nil {
		return display.ReadBack{}, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	svc, chip, release, err := p.open(ctx, id)
	if err != nil {
		return display.ReadBack{}, err
	}
	defer release()

	var cur, max C.uint16_t
	var supported C.int
	raw := make([]C.uint8_t, 12)
	rc := int(C.sd_ddc_read(svc, chip, C.uint8_t(vcp),
		boolToC(alt), &cur, &max, &supported, &raw[0], C.int(len(raw))))
	if rc != 0 {
		// 通信失败不等于「不支持」，但也不该让上层报错中断流程 —— 读回本来就是
		// best-effort。返回 Supported=false 让调用方走 applied_unconfirmed。
		return display.ReadBack{Supported: false}, nil
	}
	goRaw := make([]byte, 0, len(raw))
	for i := range raw {
		goRaw = append(goRaw, byte(raw[i]))
	}
	return display.ReadBack{
		Supported: supported != 0,
		Current:   uint16(cur),
		Maximum:   uint16(max),
		Raw:       goRaw,
	}, nil
}

// WriteVCP 写入一个 VCP 值。
func (p *Port) WriteVCP(ctx context.Context, id display.DisplayID, v display.InputValue) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	svc, chip, release, err := p.open(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	rc := int(C.sd_ddc_write(svc, chip, C.uint8_t(v.VCP), C.uint16_t(v.Value),
		boolToC(v.Alt)))
	if rc != 0 {
		return fmt.Errorf("%w: ioreturn 0x%x", display.ErrWriteFailed, rc)
	}
	return nil
}

// open 为指定显示器建立 DDC 通道。返回的 release 必须被调用。
//
// 每次读/写都重新打开通道，而不是缓存：显示器热插拔后旧的
// IOAVServiceRef 会失效，缓存反而会引入难以排查的「切一次好一次坏」。
func (p *Port) open(ctx context.Context, id display.DisplayID) (svc C.SD_IOAVServiceRef, chip C.uint32_t, release func(), err error) {
	var none C.SD_IOAVServiceRef
	adapter, err := p.findAdapter(ctx, id)
	if err != nil {
		return none, 0, nil, err
	}
	defer C.IOObjectRelease(adapter)

	svc = C.sd_open_transport(adapter, &chip)
	if svc == none {
		// 没有匹配的 DCPAVServiceProxy（比如走的是虚拟通道），退回默认通道。
		svc = C.sd_default_service()
		if svc == none {
			return none, 0, nil, fmt.Errorf("%w: no DDC transport for display %s",
				display.ErrUnsupported, id)
		}
		chip = C.SD_DDC_CHIP_ADDRESS_DEFAULT
	}
	return svc, chip, func() { C.CFRelease(C.CFTypeRef(svc)) }, nil
}

// findAdapter 找到 DisplayID 对应的 io_service_t（display adapter）。
// 调用方负责 IOObjectRelease。
func (p *Port) findAdapter(ctx context.Context, id display.DisplayID) (C.io_service_t, error) {
	records := make([]C.SDDisplayRecord, C.SD_MAX_DISPLAYS)
	n := int(C.sd_list_displays(&records[0], C.int(len(records))))
	if n < 0 {
		return C.io_service_t(0), fmt.Errorf("enumerate displays: code %d", n)
	}
	defer C.sd_release_displays(&records[0], C.int(n))

	want := string(id)
	for i := 0; i < n; i++ {
		if C.GoString(&records[i].uuid[0]) != want {
			continue
		}
		// sd_release_displays 之后会释放 adapter；这里额外 retain 一份给调用方，
		// 由 open() 负责 release。
		if C.IOObjectRetain(records[i].adapter) != 0 {
			return C.io_service_t(0), display.ErrDisplayNotFound
		}
		return records[i].adapter, nil
	}
	return C.io_service_t(0), fmt.Errorf("%w: %s", display.ErrDisplayNotFound, want)
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
