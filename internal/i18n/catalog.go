// Package i18n provides translations and locale-aware formatting for the web UI.
package i18n

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Locale identifies a supported language.
type Locale string

const (
	// German is the default locale.
	German Locale = "de"
	// English is the secondary locale.
	English Locale = "en"
)

// DefaultLocale is used when no valid locale is configured.
const DefaultLocale = German

// Catalog holds translations keyed by locale and message key.
type Catalog map[Locale]map[string]string

// NewCatalog returns the complete translation catalog for the application.
func NewCatalog() Catalog {
	return Catalog{
		German: {
			// Navigation
			"NavToday":    "Heute",
			"NavWeek":     "Woche",
			"NavMonth":    "Monat",
			"NavBooking":  "Buchung",
			"NavClosures": "Abschlüsse",
			"NavSettings": "Einstellungen",

			// Page titles
			"PageToday":         "Heute",
			"PageWeek":          "Woche",
			"PageMonth":         "Monat",
			"PageBooking":       "Buchung",
			"PageClosures":      "Abschlüsse",
			"PageClosure":       "Abschluss",
			"PageClosureDetail": "Abschlussdetails",
			"PageSettings":      "Einstellungen",
			"PageDay":           "Tag",

			// Today / day view
			"TodayDate":           "Heute - {{.Date}}",
			"Status":              "Status",
			"CurrentBlockStarted": "Aktueller Block gestartet um {{.Time}} ({{.Minutes}} Min. bisher)",
			"Worked":              "Gearbeitet",
			"Pause":               "Pause",
			"Booking":             "Buchung",
			"Booked":              "gebucht",
			"NotBooked":           "nicht gebucht",
			"ChangedAfterBooking": "Achtung: Dieser Tag wurde nach der Buchung geändert.",
			"WorkBlocks":          "Arbeitsblöcke",
			"NoWorkBlocks":        "Noch keine Arbeitsblöcke aufgezeichnet.",
			"Start":               "Start",
			"End":                 "Ende",
			"Source":              "Quelle",
			"SourceDetected":      "Automatisch",
			"SourceManual":        "Manuell",
			"Note":                "Notiz",
			"Actions":             "Aktionen",
			"Ignore":              "Ignorieren",
			"AddManualBlock":      "Manuellen Block hinzufügen",
			"Add":                 "Hinzufügen",
			"MarkDayBooked":       "Tag als gebucht markieren",
			"MarkDayNotBooked":    "Tag als nicht gebucht markieren",

			// Activity status labels (activity.ActivityState values)
			"StatusActive":    "Aktiv",
			"StatusIdle":      "Inaktiv",
			"StatusLocked":    "Gesperrt",
			"StatusSuspended": "Ruhezustand",
			"StatusUnknown":   "Unbekannt",

			// Week view
			"WeekRange":     "Woche {{.Start}} - {{.End}}",
			"TotalTracked":  "Gesamt",
			"BookedDays":    "Gebuchte Tage",
			"UnbookedDays":  "Ungebuchte Tage",
			"Day":           "Tag",
			"WorkedMinutes": "{{.Minutes}} Min.",
			"PauseMinutes":  "{{.Minutes}} Min.",
			"Blocks":        "Blöcke",
			"Changed":       "Geändert",
			"Yes":           "ja",
			"No":            "nein",

			// Month view
			"Previous": "Zurück",
			"Next":     "Weiter",

			// Booking view
			"BookingDay":            "Buchungstag",
			"CurrentBookingDay":     "Aktueller Buchungstag",
			"NoBookingDaySet":       "Kein Buchungstag gesetzt.",
			"DaysRemaining":         "{{.Days}} Tage verbleibend",
			"BookingDayOverdue":     "Achtung: Der Buchungstag ist überschritten und die Periode ist noch offen.",
			"RemoveBookingDay":      "Buchungstag entfernen",
			"SetBookingDay":         "Buchungstag setzen",
			"Date":                  "Datum",
			"Set":                   "Setzen",
			"OpenBookingPeriod":     "Offene Buchungsperiode",
			"Period":                "Periode",
			"MarkSelectedBooked":    "Ausgewählte als gebucht markieren",
			"MarkSelectedNotBooked": "Ausgewählte als nicht gebucht markieren",
			"ClosePeriod":           "Periode abschließen",
			"PreviewClosure":        "Abschlussvorschau",
			"SelectAll":             "Alle auswählen",

			// Closures
			"HistoricalClosures": "Vergangene Abschlüsse",
			"NoClosuresYet":      "Noch keine Abschlüsse.",
			"Closed":             "Abgeschlossen",
			"Days":               "Tage",
			"Unbooked":           "Ungebucht",
			"Minutes":            "Minuten",
			"Differs":            "Abweichend",

			// Close preview
			"ClosePeriodPreview":  "Abschlussvorschau",
			"TrackedWorkdays":     "Erfasste Arbeitstage",
			"TotalMinutes":        "Gesamtminuten",
			"UnbookedDaysWarning": "Achtung: {{.Count}} ungebuchte Tage.",
			"AcknowledgeUnbooked": "Ich bestätige, dass ungebuchte Tage verbleiben.",
			"CloseBookingPeriod":  "Buchungsperiode abschließen",

			// Closure detail
			"Closure":            "Abschluss",
			"ClosedAt":           "Abgeschlossen am",
			"CurrentDataDiffers": "Aktuelle Daten weichen von diesem Snapshot ab.",
			"DaySnapshot":        "Tages-Snapshot",
			"TrackedMinutes":     "Erfasste Minuten",

			// Settings
			"PollInterval":      "Abfrageintervall",
			"IdleThreshold":     "Inaktivitätsschwelle",
			"TailCredit":        "Nachlaufgutschrift",
			"ListenAddress":     "Listen-Adresse",
			"Timezone":          "Zeitzone",
			"UseSystemTimezone": "Systemzeitzone verwenden",
			"Language":          "Sprache",
			"Save":              "Speichern",
			"SettingsPersisted": "Einstellungen werden gespeichert in {{.Path}}",
		},
		English: {
			// Navigation
			"NavToday":    "Today",
			"NavWeek":     "Week",
			"NavMonth":    "Month",
			"NavBooking":  "Booking",
			"NavClosures": "Closures",
			"NavSettings": "Settings",

			// Page titles
			"PageToday":         "Today",
			"PageWeek":          "Week",
			"PageMonth":         "Month",
			"PageBooking":       "Booking",
			"PageClosures":      "Closures",
			"PageClosure":       "Closure",
			"PageClosureDetail": "Closure Detail",
			"PageSettings":      "Settings",
			"PageDay":           "Day",

			// Today / day view
			"TodayDate":           "Today - {{.Date}}",
			"Status":              "Status",
			"CurrentBlockStarted": "Current block started at {{.Time}} ({{.Minutes}} min so far)",
			"Worked":              "Worked",
			"Pause":               "Pause",
			"Booking":             "Booking",
			"Booked":              "booked",
			"NotBooked":           "not booked",
			"ChangedAfterBooking": "Warning: this day was changed after booking.",
			"WorkBlocks":          "Work blocks",
			"NoWorkBlocks":        "No work blocks recorded yet.",
			"Start":               "Start",
			"End":                 "End",
			"Source":              "Source",
			"SourceDetected":      "Automatic",
			"SourceManual":        "Manual",
			"Note":                "Note",
			"Actions":             "Actions",
			"Ignore":              "Ignore",
			"AddManualBlock":      "Add manual block",
			"Add":                 "Add",
			"MarkDayBooked":       "Mark day as booked",
			"MarkDayNotBooked":    "Mark day as not booked",

			// Activity status labels (activity.ActivityState values)
			"StatusActive":    "Active",
			"StatusIdle":      "Idle",
			"StatusLocked":    "Locked",
			"StatusSuspended": "Suspended",
			"StatusUnknown":   "Unknown",

			// Week view
			"WeekRange":     "Week {{.Start}} - {{.End}}",
			"TotalTracked":  "Total tracked",
			"BookedDays":    "Booked days",
			"UnbookedDays":  "Unbooked days",
			"Day":           "Day",
			"WorkedMinutes": "{{.Minutes}} min",
			"PauseMinutes":  "{{.Minutes}} min",
			"Blocks":        "Blocks",
			"Changed":       "Changed",
			"Yes":           "yes",
			"No":            "no",

			// Month view
			"Previous": "Previous",
			"Next":     "Next",

			// Booking view
			"BookingDay":            "Booking Day",
			"CurrentBookingDay":     "Current Booking Day",
			"NoBookingDaySet":       "No Booking Day set.",
			"DaysRemaining":         "{{.Days}} days remaining",
			"BookingDayOverdue":     "Warning: Booking Day has passed and the period is still open.",
			"RemoveBookingDay":      "Remove Booking Day",
			"SetBookingDay":         "Set Booking Day",
			"Date":                  "Date",
			"Set":                   "Set",
			"OpenBookingPeriod":     "Open Booking Period",
			"Period":                "Period",
			"MarkSelectedBooked":    "Mark selected as booked",
			"MarkSelectedNotBooked": "Mark selected as not booked",
			"ClosePeriod":           "Close Period",
			"PreviewClosure":        "Preview closure",
			"SelectAll":             "Select all",

			// Closures
			"HistoricalClosures": "Historical Closures",
			"NoClosuresYet":      "No closures yet.",
			"Closed":             "Closed",
			"Days":               "Days",
			"Unbooked":           "Unbooked",
			"Minutes":            "Minutes",
			"Differs":            "Differs",

			// Close preview
			"ClosePeriodPreview":  "Close Period Preview",
			"TrackedWorkdays":     "Tracked workdays",
			"TotalMinutes":        "Total minutes",
			"UnbookedDaysWarning": "Warning: {{.Count}} unbooked days.",
			"AcknowledgeUnbooked": "I acknowledge that unbooked days remain.",
			"CloseBookingPeriod":  "Close booking period",

			// Closure detail
			"Closure":            "Closure",
			"ClosedAt":           "Closed at",
			"CurrentDataDiffers": "Current data differs from this snapshot.",
			"DaySnapshot":        "Day snapshot",
			"TrackedMinutes":     "Tracked minutes",

			// Settings
			"PollInterval":      "Poll interval",
			"IdleThreshold":     "Idle threshold",
			"TailCredit":        "Tail credit",
			"ListenAddress":     "Listen address",
			"Timezone":          "Timezone",
			"UseSystemTimezone": "Use system timezone",
			"Language":          "Language",
			"Save":              "Save",
			"SettingsPersisted": "Settings are persisted to {{.Path}}",
		},
	}
}

// T returns the translation for the given locale and key. If the key is missing,
// it falls back to the English text and finally returns the key itself.
func (c Catalog) T(locale Locale, key string) string {
	if msg, ok := c[locale][key]; ok {
		return msg
	}
	if msg, ok := c[English][key]; ok {
		return msg
	}
	return key
}

// Tf formats a translation that contains placeholders. Placeholders use the
// form {{.Name}} purely as a textual marker; this is plain string substitution
// via strings.ReplaceAll, not text/template execution. Unknown placeholders are
// left untouched, and missing variables simply remain in the output.
func (c Catalog) Tf(locale Locale, key string, vars map[string]any) string {
	msg := c.T(locale, key)
	for k, v := range vars {
		msg = strings.ReplaceAll(msg, "{{."+k+"}}", fmt.Sprint(v))
	}
	return msg
}

// StatusLabel returns the localized label for an activity state or event type.
// The lookup key is derived as "Status"+PascalCase(status); unknown values fall
// back to the localized "Unknown" label.
func (c Catalog) StatusLabel(locale Locale, status string) string {
	key := "Status" + pascalCase(status)
	if label := c.T(locale, key); label != key {
		return label
	}
	return c.T(locale, "StatusUnknown")
}

// SourceLabel returns the localized label for a work-block source. The lookup
// key is derived as "Source"+PascalCase(source); unknown values are returned
// verbatim.
func (c Catalog) SourceLabel(locale Locale, source string) string {
	key := "Source" + pascalCase(source)
	if label := c.T(locale, key); label != key {
		return label
	}
	return source
}

// pascalCase converts a snake_case, kebab-case or space-separated identifier to
// PascalCase, e.g. "service_started" becomes "ServiceStarted".
func pascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var b strings.Builder
	for _, p := range parts {
		r, size := utf8.DecodeRuneInString(p)
		if r == utf8.RuneError && size == 0 {
			continue
		}
		b.WriteString(string(unicode.ToUpper(r)))
		b.WriteString(p[size:])
	}
	return b.String()
}

// SupportedLocales returns the list of supported locale codes.
func SupportedLocales() []Locale {
	return []Locale{German, English}
}

// NativeName returns the human-readable name of the locale in its own language.
func (l Locale) NativeName() string {
	switch l {
	case German:
		return "Deutsch"
	case English:
		return "English"
	default:
		return string(l)
	}
}

// ParseLocale converts a string to a supported locale, falling back to the default.
func ParseLocale(s string) Locale {
	switch Locale(strings.ToLower(strings.TrimSpace(s))) {
	case German:
		return German
	case English:
		return English
	default:
		return DefaultLocale
	}
}
