package validate

import "testing"

func TestPhone(t *testing.T) {
	cases := map[string]string{
		"8 701 245 18 90":      "+77012451890",
		"+7 (701) 245-18-90":   "+77012451890",
		"77012451890":          "+77012451890",
		"7012451890":           "+77012451890",
		"":                     "",
	}
	for in, want := range cases {
		got, ok := Phone(in)
		if !ok || got != want {
			t.Errorf("Phone(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := Phone("12345"); ok {
		t.Error("short number must be rejected")
	}
}

func TestIIN(t *testing.T) {
	// контрольная цифра: sum(n[i]*(i+1)) % 11 для 09031450451_ = 191 % 11 = 4
	if !IIN("090314504514") {
		t.Error("valid IIN rejected")
	}
	for _, bad := range []string{"090314504512", "12345678901", "abcdefghijkl", "1234567890123"} {
		if IIN(bad) {
			t.Errorf("invalid IIN %q accepted", bad)
		}
	}
	if MaskIIN("090314504514") != "•••• •••• 4514" {
		t.Error("mask")
	}
}

func TestEmail(t *testing.T) {
	if !Email("a@b.kz") || !Email("") || Email("not-an-email") {
		t.Error("email validation")
	}
}
