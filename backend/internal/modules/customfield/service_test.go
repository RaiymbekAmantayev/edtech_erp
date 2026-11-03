package customfield

import (
	"net/url"
	"strings"
	"testing"

	"emp/internal/platform/auth"
)

func f64(v float64) *float64 { return &v }

func field(typ string) *Field {
	return &Field{Key: "k", Type: typ, Label: map[string]string{"ru": "Поле"}, Options: []Option{
		{ID: "opt1", Label: map[string]string{"ru": "11"}},
		{ID: "opt2", Label: map[string]string{"ru": "Физика"}},
	}}
}

func TestNormalize(t *testing.T) {
	num := field("integer")
	num.Settings.Min, num.Settings.Max = f64(0), f64(140)
	cases := []struct {
		f    *Field
		in   any
		want any
		code string
	}{
		{num, float64(128), int64(128), ""},
		{num, "125", int64(125), ""},
		{num, float64(200), nil, "TOO_LARGE"},
		{num, 12.5, nil, "INVALID_NUMBER"},
		{field("money"), float64(1800000), int64(1800000), ""},
		{field("money"), 18000.5, nil, "INVALID_MONEY"}, // деньги только целые тиын, без float
		{field("date"), "2026-11-02", "2026-11-02", ""},
		{field("date"), "02.11.2026", nil, "INVALID_DATE"},
		{field("boolean"), true, true, ""},
		{field("select"), "opt1", "opt1", ""},
		{field("select"), "nope", nil, "INVALID_OPTION"},
		{field("phone"), "8 701 111 22 33", "+77011112233", ""},
		{field("url"), "javascript:alert(1)", nil, "INVALID_URL"},
		{field("url"), "https://zoom.us/j/1", "https://zoom.us/j/1", ""},
		{field("string"), strings.Repeat("я", 256), nil, "TOO_LONG"},
	}
	for i, c := range cases {
		got, code := normalize(c.f, c.in)
		if code != c.code || (c.code == "" && got != c.want) {
			t.Errorf("#%d %s(%v): got %v %q, want %v %q", i, c.f.Type, c.in, got, code, c.want, c.code)
		}
	}
	ms, code := normalize(field("multiselect"), []any{"opt1", "opt2", "opt1"})
	if code != "" || len(ms.([]string)) != 2 {
		t.Errorf("multiselect dedupe: %v %q", ms, code)
	}
}

func TestRoleVisibility(t *testing.T) {
	fields := []Field{
		{Key: "health", Type: "text", IsSensitive: true, Settings: Settings{RolesView: []string{"org_admin"}}},
		{Key: "school", Type: "string"},
		{Key: "contract", Type: "string", IsSensitive: true},
	}
	teacher := &auth.Principal{RoleCodes: []string{"teacher"}}
	vals := map[string]any{"health": "астма", "school": "НИШ", "contract": "Д-1"}
	out := Strip(fields, vals, teacher, true)
	if _, ok := out["health"]; ok {
		t.Error("teacher must not see field restricted to org_admin")
	}
	if out["school"] != "НИШ" || out["contract"] != Masked {
		t.Errorf("unexpected strip result: %v", out)
	}
	owner := &auth.Principal{RoleCodes: []string{"owner"}}
	if Strip(fields, vals, owner, false)["health"] != "астма" {
		t.Error("owner sees everything")
	}
}

func TestFilterSQLRejectsUnknownAndUsesParams(t *testing.T) {
	fields := []Field{*field("integer"), {Key: "grade", Type: "select", Options: []Option{{ID: "o11", Label: map[string]string{"ru": "11"}}}}}
	fields[0].Key = "target_score"
	p := &auth.Principal{RoleCodes: []string{"owner"}}
	args := []any{}
	q := url.Values{"cf.target_score[gte]": {"110"}, "cf.grade": {"11"}}
	conds, err := FilterSQL(fields, q, "s.custom_fields", &args, p)
	if err != nil || len(conds) != 2 {
		t.Fatalf("conds=%v err=%v", conds, err)
	}
	for _, c := range conds {
		if strings.Contains(c, "110") || strings.Contains(c, "o11") {
			t.Errorf("values must be passed as parameters, got %q", c)
		}
	}
	if _, err := FilterSQL(fields, url.Values{"cf.drop_table": {"1"}}, "s.custom_fields", &args, p); err == nil {
		t.Error("unknown field filter must be rejected")
	}
}

func TestGenerateKey(t *testing.T) {
	if k := GenerateKey("Целевой балл ЕНТ"); k != "tselevoy_ball_ent" {
		t.Errorf("got %q", k)
	}
	if !ValidKey("target_score") || ValidKey("phone") || ValidKey("1abc") {
		t.Error("ValidKey")
	}
}
