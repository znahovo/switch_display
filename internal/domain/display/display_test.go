package display

import "testing"

func TestDisplayLabelFallsBackInOrder(t *testing.T) {
	cases := []struct {
		d    Display
		want string
	}{
		{Display{ID: "uuid-1", Name: "Mi Monitor"}, "Mi Monitor"},
		{Display{ID: "uuid-1"}, "Display uuid-1"},
		{Display{}, "Unknown Display"},
	}
	for _, c := range cases {
		if got := c.d.Label(); got != c.want {
			t.Errorf("Label() = %q, want %q", got, c.want)
		}
	}
}

func TestVCPCodeString(t *testing.T) {
	if got := VCPInputSource.String(); got != "0x60" {
		t.Errorf("VCPInputSource.String() = %q, want 0x60", got)
	}
	if got := VCPInputSourceAlt.String(); got != "0xF4" {
		t.Errorf("VCPInputSourceAlt.String() = %q, want 0xF4", got)
	}
	// 小写十六进制也要补零到两位。
	if got := VCPCode(0x0a).String(); got != "0x0A" {
		t.Errorf("VCPCode(0x0a).String() = %q, want 0x0A", got)
	}
}

func TestInputValueString(t *testing.T) {
	std := InputValue{VCP: VCPInputSource, Value: 17}
	if got, want := std.String(), "vcp=0x60 value=17(0x11)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	alt := InputValue{VCP: VCPInputSourceAlt, Value: 208, Alt: true}
	if got, want := alt.String(), "alt vcp=0xF4 value=208(0xd0)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
