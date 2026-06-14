package i18n

import (
	"strings"
	"testing"
	"time"
)

func TestCatalog_T(t *testing.T) {
	cat := NewCatalog()
	if got := cat.T(German, "NavToday"); got != "Heute" {
		t.Errorf("German NavToday = %q, want Heute", got)
	}
	if got := cat.T(English, "NavToday"); got != "Today" {
		t.Errorf("English NavToday = %q, want Today", got)
	}
}

func TestCatalog_TFallback(t *testing.T) {
	cat := NewCatalog()
	// Unknown locale falls back to English, then to the key itself.
	if got := cat.T("fr", "NavToday"); got != "Today" {
		t.Errorf("fallback NavToday = %q, want Today", got)
	}
	if got := cat.T(German, "MissingKey"); !strings.Contains(got, "MissingKey") {
		t.Errorf("missing key should return key, got %q", got)
	}
}

func TestCatalog_Tf(t *testing.T) {
	cat := NewCatalog()
	got := cat.Tf(German, "WeekRange", map[string]any{"Start": "01.06.", "End": "07.06."})
	if got != "Woche 01.06. - 07.06." {
		t.Errorf("Tf = %q, want Woche 01.06. - 07.06.", got)
	}
}

func TestCatalog_SourceLabels(t *testing.T) {
	cat := NewCatalog()
	if got := cat.T(German, "SourceDetected"); got != "Automatisch" {
		t.Errorf("German SourceDetected = %q, want Automatisch", got)
	}
	if got := cat.T(German, "SourceManual"); got != "Manuell" {
		t.Errorf("German SourceManual = %q, want Manuell", got)
	}
}

func TestParseLocale(t *testing.T) {
	if got := ParseLocale("de"); got != German {
		t.Errorf("ParseLocale(de) = %q, want de", got)
	}
	if got := ParseLocale("EN"); got != English {
		t.Errorf("ParseLocale(EN) = %q, want en", got)
	}
	if got := ParseLocale("invalid"); got != DefaultLocale {
		t.Errorf("ParseLocale(invalid) = %q, want default", got)
	}
}

func TestDateShort(t *testing.T) {
	d := time.Date(2026, time.June, 14, 0, 0, 0, 0, time.UTC)
	if got := DateShort(d, German); got != "14.06.2026" {
		t.Errorf("DateShort(de) = %q, want 14.06.2026", got)
	}
	if got := DateShort(d, English); got != "2026-06-14" {
		t.Errorf("DateShort(en) = %q, want 2026-06-14", got)
	}
}

func TestMonthYear(t *testing.T) {
	d := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	if got := MonthYear(d, German); got != "Juni 2026" {
		t.Errorf("MonthYear(de) = %q, want Juni 2026", got)
	}
	if got := MonthYear(d, English); got != "June 2026" {
		t.Errorf("MonthYear(en) = %q, want June 2026", got)
	}
}
