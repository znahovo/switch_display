//go:build manual && darwin && cgo

// 这个文件只在 `go test -tags manual` 下参与编译。
//
// 它需要一台真实的、支持 DDC/CI 的显示器：会往当前输入口写一次同样的值
// （屏幕不会切走），并检查「写 → 等 → 读回 → 判定」整条链路。
// 之所以保留在仓库里而不是删掉：它是唯一能验证 cgo 层与用例层拼起来
// 真的能工作的测试，改 DDC 代码时值得跑一遍。
package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	appcore "github.com/znahovo/switch-display/internal/application"
	"github.com/znahovo/switch-display/internal/application/configapp"
	"github.com/znahovo/switch-display/internal/application/displayapp"
	"github.com/znahovo/switch-display/internal/application/switchapp"
	"github.com/znahovo/switch-display/internal/domain/display"
	"github.com/znahovo/switch-display/internal/domain/source"
	"github.com/znahovo/switch-display/internal/domain/switching"
	"github.com/znahovo/switch-display/internal/infrastructure/ddc"
	"github.com/znahovo/switch-display/internal/infrastructure/persistence"
	"github.com/znahovo/switch-display/internal/infrastructure/platform"
)

// nopEvents 吞掉事件（测试不关心广播）。
type nopEvents struct{}

func (nopEvents) Emit(string, any) {}

// nopHotkeys 满足 HotkeyRegistrar，不做任何事。
type nopHotkeys struct{}

func (nopHotkeys) RegisterAll(b []appcore.Binding) []error { return make([]error, len(b)) }
func (nopHotkeys) UnregisterAll()                          {}

// TestSwitchToCurrentPort 写回当前输入口，验证整条链路。
func TestSwitchToCurrentPort(t *testing.T) {
	ctx := context.Background()
	log := platform.NewLogger(nil)
	clock := platform.NewClock()
	port := ddc.New()

	store, err := persistence.NewJSONStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	displays := displayapp.New(displayapp.Deps{Port: port, Log: log})
	list, err := displays.Refresh(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(list) == 0 {
		t.Skip("no display attached")
	}
	d := list[0]
	t.Logf("display %s (%s)", d.Name, d.ID)

	rb, err := port.ReadVCP(ctx, d.ID, display.VCPInputSource, false)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	if !rb.Supported {
		t.Skip("display does not report its current input")
	}
	t.Logf("current input: value=%d", rb.Current)

	engine := switching.NewService(port, clock)
	cfg := configapp.New(configapp.Deps{
		Repo: store, Export: store, Events: nopEvents{}, Hotkey: nopHotkeys{}, Log: log, Clock: clock,
	})
	sw := switchapp.New(switchapp.Deps{Engine: engine, Config: cfg, Events: nopEvents{}, Log: log})

	src, err := source.New("self-check", d.ID, display.InputValue{
		VCP: display.VCPInputSource, Value: rb.Current,
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := source.EmptySnapshot()
	snap.Sources = append(snap.Sources, src)
	if _, err := cfg.Save(ctx, snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	res, err := sw.SwitchByID(ctx, string(src.ID))
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	t.Logf("outcome=%s attempts=%d current=%d message=%q",
		res.DTO.Outcome, res.DTO.Attempts, res.DTO.Current, res.DTO.Message)
	if res.DTO.Outcome != "confirmed" && res.DTO.Outcome != "applied_unconfirmed" {
		t.Fatalf("unexpected outcome: %s", res.DTO.Outcome)
	}

	back, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(back.Sources) != 1 || back.Sources[0].Name != "self-check" {
		t.Fatalf("roundtrip failed: %+v", back.Sources)
	}
}
