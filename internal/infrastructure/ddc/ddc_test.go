package ddc

import "testing"

func TestNewReturnsPort(t *testing.T) {
	// 平台工厂至少要能构造出非 nil 的实现，否则启动期就会 panic。
	if p := New(); p == nil {
		t.Fatal("New() returned nil")
	}
}
