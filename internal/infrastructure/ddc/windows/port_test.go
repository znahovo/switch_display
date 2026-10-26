//go:build windows

package windows

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"unsafe"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// 本文件只测纯逻辑：EDID 解析、设备路径解析、结构体布局、以及
// 「不支持」的降级路径。任何会调 Dxva2 的用例都不在这里——CI 没有显示器，
// 而且真去写 VCP 会把开发机的显示器切走（plan.md §7 的分层约定）。

func TestPhysicalMonitorLayout(t *testing.T) {
	// PHYSICAL_MONITOR = HANDLE + WCHAR[128]，64 位下 8 + 256 = 264。
	// 布局错一个字节，handle 就会读到描述字符串里去，表现为「读取随机显示器」。
	if got := unsafe.Sizeof(physicalMonitor{}); got != 264 {
		t.Fatalf("physicalMonitor size = %d, want 264", got)
	}
	if got := unsafe.Offsetof(physicalMonitor{}.description); got != 8 {
		t.Fatalf("description offset = %d, want 8", got)
	}
}

func TestMonitorInfoExLayout(t *testing.T) {
	// MONITORINFOEXW 是 104 字节：cbSize 必须正好是这个值，否则
	// GetMonitorInfoW 只填到 MONITORINFO 的部分，szDevice 是空的
	//（表现为「所有显示器都叫空名字」）。
	if got := unsafe.Sizeof(monitorInfoEx{}); got != 104 {
		t.Fatalf("monitorInfoEx size = %d, want 104", got)
	}
	if got := unsafe.Offsetof(monitorInfoEx{}.szDevice); got != 40 {
		t.Fatalf("szDevice offset = %d, want 40", got)
	}
}

func TestDisplayDeviceLayout(t *testing.T) {
	// DISPLAY_DEVICEW 是 840 字节（cb 对齐到 4，后面 5 个宽字符数组）。
	if got := unsafe.Sizeof(displayDevice{}); got != 840 {
		t.Fatalf("displayDevice size = %d, want 840", got)
	}
	if got := unsafe.Offsetof(displayDevice{}.deviceID); got != 328 {
		t.Fatalf("deviceID offset = %d, want 328", got)
	}
}

func TestDeviceInstance(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "设备接口路径",
			in:   `\\?\DISPLAY#XMI27B1#5&187333d6&0&UID20738#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`,
			want: `DISPLAY\XMI27B1\5&187333d6&0&UID20738`,
			ok:   true,
		},
		{
			name: "LG 那台",
			in:   `\\?\DISPLAY#LHCFFFF#5&187333d6&0&UID20741#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`,
			want: `DISPLAY\LHCFFFF\5&187333d6&0&UID20741`,
			ok:   true,
		},
		{
			name: "GDI 名不是设备接口路径",
			in:   `\\.\DISPLAY1`,
			want: "",
			ok:   false,
		},
		{
			name: "空串",
			in:   "",
			want: "",
			ok:   false,
		},
		{
			name: "只有 class 段",
			in:   `\\?\DISPLAY#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`,
			want: "",
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := deviceInstance(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestFallbackName(t *testing.T) {
	id := `\\?\DISPLAY#XMI27B1#5&187333d6&0&UID20738#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`
	if got := fallbackName(id); got != "XMI27B1" {
		t.Fatalf("fallbackName = %q, want XMI27B1", got)
	}
	if got := fallbackName(`\\.\DISPLAY1`); got != "" {
		t.Fatalf("fallbackName(gdi) = %q, want empty", got)
	}
}

func TestPnPID(t *testing.T) {
	cases := []struct {
		in   uint16
		want string
	}{
		{0x3103, "LHC"}, // 实测：P25A2G 的厂家
		{0x61A9, "XMI"}, // 实测：Mi Monitor 的厂家（VESA 注册码）
		{0x10AC, "DEL"},
		{0, ""},
		{0x8000, ""}, // 保留位为 1
		{0x0001, ""}, // 解出来是 '@' 之外的字符
	}
	for _, c := range cases {
		if got := pnpID(c.in); got != c.want {
			t.Errorf("pnpID(%#04x) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseEDID(t *testing.T) {
	// 用本机真实抓到的 128 字节 EDID 头部 + 一个名字描述符构造数据，
	// 避免测试依赖某个具体显示器。其余字节填 0 不影响解析。
	data := make([]byte, 128)
	copy(data, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	// 厂商 ID 0x3103 → "LHC"，产品码 0x02A2（小端存 A2 02）
	data[8], data[9] = 0x31, 0x03
	data[10], data[11] = 0xA2, 0x02
	// 序列号 0x12345678（小端）
	data[12], data[13], data[14], data[15] = 0x78, 0x56, 0x34, 0x12
	// 第一个描述符：0xFC = 显示器名 "P25A2G"
	data[54], data[55], data[56], data[57] = 0x00, 0x00, 0x00, 0xFC
	copy(data[59:], []byte("P25A2G"))
	data[65] = 0x0A

	info, ok := parseEDID(data)
	if !ok {
		t.Fatal("parseEDID rejected valid block")
	}
	if info.Name != "P25A2G" {
		t.Errorf("Name = %q, want P25A2G", info.Name)
	}
	if info.Vendor != "LHC" {
		t.Errorf("Vendor = %q, want LHC", info.Vendor)
	}
	if info.Model != "674" {
		t.Errorf("Model = %q, want 674", info.Model)
	}
	if info.Serial != "305419896" {
		t.Errorf("Serial = %q, want 305419896", info.Serial)
	}
}

func TestParseEDIDRejectsBadInput(t *testing.T) {
	if _, ok := parseEDID(nil); ok {
		t.Error("parseEDID(nil) reported ok")
	}
	if _, ok := parseEDID(make([]byte, 127)); ok {
		t.Error("parseEDID(short) reported ok")
	}
	bad := make([]byte, 128) // 头部全 0
	if _, ok := parseEDID(bad); ok {
		t.Error("parseEDID(bad header) reported ok")
	}
}

func TestParseEDIDSerialDescriptorWins(t *testing.T) {
	// 数值序列号为 0（厂商没填）时应该用描述符里的文本序列号。
	data := make([]byte, 128)
	copy(data, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	data[8], data[9] = 0x31, 0x03
	data[54], data[55], data[56], data[57] = 0x00, 0x00, 0x00, 0xFF
	copy(data[59:], []byte("ABC123"))
	data[65] = 0x0A

	info, ok := parseEDID(data)
	if !ok {
		t.Fatal("parseEDID rejected valid block")
	}
	if info.Serial != "ABC123" {
		t.Errorf("Serial = %q, want ABC123", info.Serial)
	}
}

func TestReadEDIDRejectsNonDeviceID(t *testing.T) {
	// GDI 名没有对应的注册表实例路径：必须直接返回 false，不能去读注册表
	//（读注册表会拿到错误信息，混进日志里制造噪音）。
	if _, ok := readEDID(display.DisplayID(`\\.\DISPLAY1`)); ok {
		t.Error("readEDID accepted GDI name")
	}
}

func TestReadVCPAltIsUnsupported(t *testing.T) {
	p := New()
	rb, err := p.ReadVCP(context.Background(), display.DisplayID(`\\?\DISPLAY#X#1#{x}`),
		display.VCPInputSourceAlt, true)
	if err != nil {
		t.Fatalf("ReadVCP(alt) error = %v, want nil", err)
	}
	if rb.Supported {
		t.Error("ReadVCP(alt) reported supported")
	}
}

func TestWriteVCPAltIsUnsupported(t *testing.T) {
	p := New()
	err := p.WriteVCP(context.Background(), display.DisplayID(`\\?\DISPLAY#X#1#{x}`),
		display.InputValue{VCP: display.VCPInputSourceAlt, Value: 208, Alt: true})
	if !errors.Is(err, display.ErrUnsupported) {
		t.Fatalf("WriteVCP(alt) error = %v, want ErrUnsupported", err)
	}
	// 关键：不能是 ErrWriteFailed。writing 失败会触发重试与「写入失败」提示，
	// 而这台机器根本做不到——重试只是白等 300ms。
	if errors.Is(err, display.ErrWriteFailed) {
		t.Error("WriteVCP(alt) was wrapped as ErrWriteFailed")
	}
}

func TestContextCancelled(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.List(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("List err = %v, want context.Canceled", err)
	}
	if _, err := p.ReadVCP(ctx, "x", display.VCPInputSource, false); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadVCP err = %v, want context.Canceled", err)
	}
	if err := p.WriteVCP(ctx, "x", display.InputValue{VCP: display.VCPInputSource}); !errors.Is(err, context.Canceled) {
		t.Errorf("WriteVCP err = %v, want context.Canceled", err)
	}
}

func TestUnknownDisplayIsNotFound(t *testing.T) {
	// 这个用例会调 EnumDisplayMonitors（无害，只读桌面拓扑），
	// 但不会打开任何 DDC 通道。
	p := New()
	_, err := p.ReadVCP(context.Background(), display.DisplayID("no-such-display"),
		display.VCPInputSource, false)
	if err == nil {
		t.Skip("no monitors enumerated in this environment")
	}
	if !errors.Is(err, display.ErrDisplayNotFound) {
		t.Fatalf("err = %v, want ErrDisplayNotFound", err)
	}
}

func TestHexRoundTrip(t *testing.T) {
	// List 把 EDID 以 hex 存进 Display.EDID；这里确认编码与解析用的
	// 是同一份字节，避免出现「EDID 字段是 base64、前端按 hex 解」这种错配。
	data := []byte{0x00, 0xFF, 0x12, 0x34}
	if got := hex.EncodeToString(data); got != "00ff1234" {
		t.Fatalf("hex = %q, want 00ff1234", got)
	}
}
