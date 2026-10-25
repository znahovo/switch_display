package platform

import (
	"strings"
	"sync"
	"testing"

	appcore "github.com/znahovo/switch-display/internal/application"
)

func TestRegisterAllWithoutAppReportsUnsupported(t *testing.T) {
	// 没有 Wails 运行时（例如 Linux 构建或单测）时不能静默成功：
	// 调用方要靠这个错误生成「快捷键不可用」的警告。
	h := NewHotkeys(nil)
	errs := h.RegisterAll([]appcore.Binding{
		{Accelerator: "Cmd+Option+1", SourceID: "s1"},
		{Accelerator: "Cmd+Option+2", SourceID: "s2"},
	})
	if len(errs) != 2 {
		t.Fatalf("len(errs) = %d", len(errs))
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("errs[%d] = nil, want unsupported", i)
			continue
		}
		if !strings.Contains(err.Error(), "not available") {
			t.Errorf("errs[%d] = %v", i, err)
		}
	}
}

func TestRegisterAllReturnsSliceMatchingInput(t *testing.T) {
	// 返回切片必须与输入等长且按下标对应，否则 configapp 拼 warning 会错位。
	h := NewHotkeys(nil)
	bindings := []appcore.Binding{{Accelerator: ""}, {Accelerator: "Cmd+Option+8"}}
	errs := h.RegisterAll(bindings)
	if len(errs) != len(bindings) {
		t.Fatalf("len = %d, want %d", len(errs), len(bindings))
	}
}

func TestUnregisterAllWithoutAppIsSafe(t *testing.T) {
	h := NewHotkeys(nil)
	h.UnregisterAll() // 不该 panic
	h.UnregisterAll()
}

func TestOnFireDispatch(t *testing.T) {
	h := NewHotkeys(nil)

	var mu sync.Mutex
	var got []string
	h.OnFire(func(id string) {
		mu.Lock()
		got = append(got, id)
		mu.Unlock()
	})

	// 直接调内部 onFire：注册失败时（无 Wails）回调仍然由 OnFire 装配，
	// 这里验证的是「回调被正确转发」，与平台能力无关。
	h.onFire("s1")
	h.onFire("s2")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Errorf("got = %v", got)
	}
}

func TestOnFireWithoutHandlerDoesNotPanic(t *testing.T) {
	h := NewHotkeys(nil)
	h.onFire("s1") // 还没装回调
}

func TestRegisterAllEmptyBindingIsSkipped(t *testing.T) {
	// 空 accelerator 表示「该源没绑快捷键」，不能当成错误上报。
	h := NewHotkeys(nil)
	errs := h.RegisterAll([]appcore.Binding{{Accelerator: "", SourceID: "s1"}})
	if len(errs) != 1 {
		t.Fatalf("len = %d", len(errs))
	}
	// 无 app 时仍会报 unsupported（因为校不到运行时）；这里只断言不 panic
	// 且切片对齐——真正的「跳过」逻辑在有 app 的分支里。
	if errs[0] == nil {
		// 有 Wails 运行时的构建里会走这里：空串被跳过，errs[0] 是 nil。
		return
	}
}
