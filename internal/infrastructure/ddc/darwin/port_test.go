//go:build darwin && cgo

package darwin

import (
	"context"
	"testing"

	"github.com/znahovo/switch-display/internal/domain/display"
)

// TestListDisplays 只是烟雾测试：能跑通枚举路径、拿到的 ID 非空。
// 不校验条数——CI 机器上通常没有外接显示器。
func TestListDisplays(t *testing.T) {
	p := New()
	list, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, d := range list {
		if d.ID == "" {
			t.Fatalf("display with empty id: %+v", d)
		}
		t.Logf("display id=%s name=%q edid=%s builtin=%v", d.ID, d.Name, d.EDID, d.Builtin)
	}
}

// TestReadVCPDoesNotCrash 验证读一个真实存在的 VCP 不会 panic/挂住。
// 显示器不支持读回时返回 Supported=false，这不算失败。
func TestReadVCPDoesNotCrash(t *testing.T) {
	p := New()
	list, err := p.List(context.Background())
	if err != nil || len(list) == 0 {
		t.Skip("no display attached")
	}
	rb, err := p.ReadVCP(context.Background(), list[0].ID, display.VCPLuminance, false)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Logf("luminance supported=%v cur=%d max=%d raw=% x", rb.Supported, rb.Current, rb.Maximum, rb.Raw)
}
