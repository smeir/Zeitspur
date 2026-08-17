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
			"NavCopilot":  "Copilot",
			"NavSettings": "Einstellungen",

			// Page titles
			"PageToday":         "Heute",
			"PageWeek":          "Woche",
			"PageMonth":         "Monat",
			"PageBooking":       "Buchung",
			"PageClosures":      "Abschlüsse",
			"PageClosure":       "Abschluss",
			"PageClosureDetail": "Abschlussdetails",
			"PageCopilot":       "Copilot-Credits",
			"PageSettings":      "Einstellungen",
			"PageDay":           "Tag",

			// Today / day view
			"TodayDate":              "Heute - {{.Date}}",
			"Status":                 "Status",
			"CurrentBlockStarted":    "Aktueller Block gestartet um {{.Time}} ({{.Minutes}} Min. bisher)",
			"Worked":                 "Gearbeitet",
			"Pause":                  "Pause",
			"Booking":                "Buchung",
			"Booked":                 "gebucht",
			"NotBooked":              "nicht gebucht",
			"ChangedAfterBooking":    "Achtung: Dieser Tag wurde nach der Buchung geändert.",
			"WorkBlocks":             "Arbeitsblöcke",
			"NoWorkBlocks":           "Noch keine Arbeitsblöcke aufgezeichnet.",
			"Start":                  "Start",
			"End":                    "Ende",
			"Source":                 "Quelle",
			"SourceDetected":         "Automatisch",
			"SourceManual":           "Manuell",
			"Note":                   "Notiz",
			"Actions":                "Aktionen",
			"Ignore":                 "Ignorieren",
			"Unignore":               "Ignorieren zurücksetzen",
			"Delete":                 "Löschen",
			"Ignored":                "Ignoriert",
			"AddManualBlock":         "Manuellen Block hinzufügen",
			"Add":                    "Hinzufügen",
			"Cancel":                 "Abbrechen",
			"SystemActive":           "System aktiv",
			"TargetEightHours":       "Ziel 8h",
			"DailyTimeline":          "Tagesverlauf",
			"Running":                "Laufend",
			"Now":                    "Jetzt",
			"Time":                   "Zeit",
			"Duration":               "Dauer",
			"DailyProgress":          "Tagesfortschritt",
			"RemainingUntilTarget":   "noch {{.Duration}} bis Ziel",
			"ThisWeek":               "Diese Woche",
			"BookingPeriod":          "Buchungsperiode",
			"Remaining":              "Verbleibend",
			"DaysRemainingShort":     "{{.Days}} Tage",
			"OpenBookingDaysWarning": "{{.Count}} Tage sind noch nicht gebucht.",

			// Activity status labels (activity.ActivityState values)
			"StatusActive":    "Aktiv",
			"StatusIdle":      "Inaktiv",
			"StatusLocked":    "Gesperrt",
			"StatusSuspended": "Ruhezustand",
			"StatusUnknown":   "Unbekannt",

			// Week view
			"ISOWeekShort": "KW {{.Week}}",
			"WeekRange":    "Woche {{.Start}} - {{.End}}",
			"CurrentWeek":  "Aktuelle Woche",
			"TotalTracked": "Gesamt",
			"BookedDays":   "Gebuchte Tage",
			"UnbookedDays": "Ungebuchte Tage",
			"Day":          "Tag",
			"Blocks":       "Blöcke",
			"Changed":      "Geändert",
			"Yes":          "ja",
			"No":           "nein",

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
			"Differs":            "Abweichend",

			// Close preview
			"ClosePeriodPreview":  "Abschlussvorschau",
			"TrackedWorkdays":     "Erfasste Arbeitstage",
			"TotalDuration":       "Gesamtdauer",
			"UnbookedDaysWarning": "Achtung: {{.Count}} ungebuchte Tage.",
			"AcknowledgeUnbooked": "Ich bestätige, dass ungebuchte Tage verbleiben.",
			"CloseBookingPeriod":  "Buchungsperiode abschließen",

			// Closure detail
			"Closure":            "Abschluss",
			"ClosedAt":           "Abgeschlossen am",
			"CurrentDataDiffers": "Aktuelle Daten weichen von diesem Snapshot ab.",
			"DaySnapshot":        "Tages-Snapshot",
			"TrackedDuration":    "Erfasste Dauer",

			// Settings
			"Settings":                "Einstellungen",
			"SettingsSections":        "Einstellungsbereiche",
			"ActivitySettings":        "Aktivität",
			"ActivityDetection":       "Aktivitätserkennung",
			"ActivityDetectionDesc":   "Wie Zeitspur Arbeits- und Pausenzeiten erkennt. GNOME/Mutter über D-Bus.",
			"ActivityMode":            "Aktivitätsmodus",
			"ActivityModeIdleAndLock": "Inaktivität und Bildschirmsperre",
			"ActivityModeLockOnly":    "Nur Bildschirmsperre",
			"PollInterval":            "Abfrageintervall",
			"PollIntervalHint":        "Wie oft der Inaktivitätsstatus abgefragt wird, zum Beispiel 5s oder 10s.",
			"IdleThreshold":           "Inaktivitätsschwelle",
			"IdleThresholdModeNote":   "Die Inaktivitätsschwelle gilt nur im Modus Inaktivität und Bildschirmsperre. Bildschirmsperre und Ruhezustand zählen immer als Pause.",
			"WorkdaySettings":         "Arbeitstage",
			"WorkdaySettingsDesc":     "An welchen Wochentagen Zeitspur automatisch auf der Heute-Seite erscheint.",
			"LocaleSettings":          "Region & Sprache",
			"LocaleSettingsDesc":      "Zeitzone und Sprache der Oberfläche.",
			"AppearanceSettings":      "Erscheinungsbild",
			"AppearanceSettingsDesc":  "Anordnung und Darstellung der Oberfläche.",
			"Navigation":              "Navigation",
			"NavigationHint":          "Position der Hauptnavigation. „Oben“ wie ein klassisches Top-Menü, „Links“ als seitliche Sidebar.",
			"NavigationTop":           "Oben",
			"NavigationSide":          "Links (Sidebar)",
			"ServerSettings":          "Server",
			"ServerSettingsDesc":      "Wo die lokale Web-Oberfläche erreichbar ist. Aus Sicherheitsgründen nur auf Localhost empfohlen.",
			"ListenAddress":           "Listen-Adresse",
			"ListenAddressHint":       "Format host:port. Für externen Zugriff sollte eine Firewall oder Authentifizierung vorgeschaltet werden.",
			"Timezone":                "Zeitzone",
			"UseSystemTimezone":       "Systemzeitzone verwenden",
			"Language":                "Sprache",
			"TodayWeekdays":           "Wochentage in Heute-Ansicht",
			"Discard":                 "Verwerfen",
			"Save":                    "Speichern",
			"Version":                 "Version",
			"SettingsPersisted":       "Einstellungen werden gespeichert in {{.Path}}",

			// Copilot credits
			"CopilotStatus":           "Aktueller Stand",
			"CopilotPlan":             "Plan",
			"CopilotOrganizations":    "Organisationen",
			"CopilotEntitlement":      "Guthaben",
			"CopilotRemaining":        "Verbleibend",
			"CopilotUsed":             "Verbraucht",
			"CopilotPercentRemaining": "Verbleibend %",
			"CopilotResetAt":          "Reset am",
			"CopilotBilling":          "Abrechnung",
			"CopilotBillingToken":     "Token-basiert",
			"CopilotBillingSeat":      "Sitze",
			"CopilotConsumption":      "Verbrauch",
			"CopilotConsumed":         "Verbraucht",
			"CopilotSamples":          "Messungen",
			"CopilotTotalConsumed":    "Gesamtverbrauch",
			"CopilotNoData":           "Noch keine Copilot-Daten. Der Fetcher läuft stündlich; erste Daten erscheinen bald.",
			"CopilotFetchError":       "Letzter Abruf fehlgeschlagen",

			// Copilot fetch error kinds (see internal/copilot.ErrorKind)
			"CopilotErrorUnavailable": "GitHub ist vorübergehend nicht erreichbar (Serverfehler). Der nächste Abruf versucht es automatisch erneut.",
			"CopilotErrorNetwork":     "Keine Verbindung zu GitHub. Netzwerk oder Proxy prüfen; der nächste Abruf versucht es automatisch erneut.",
			"CopilotErrorRateLimited": "GitHub-API-Limit erreicht. Der nächste Abruf versucht es automatisch erneut.",
			"CopilotErrorAuth":        "GitHub CLI ist nicht angemeldet. Bitte 'gh auth login' ausführen.",
			"CopilotErrorForbidden":   "GitHub verweigert den Zugriff. Token-Berechtigungen bzw. SSO-Freigabe prüfen.",
			"CopilotErrorNoSeat":      "Kein Copilot-Zugang für dieses Konto gefunden.",
			"CopilotErrorGhMissing":   "GitHub CLI (gh) nicht gefunden. Pfad in den Einstellungen prüfen.",
			"CopilotErrorParse":       "Unerwartete Antwort von GitHub (keine Quota-Daten).",
			"CopilotErrorUnknown":     "Abruf fehlgeschlagen.",
			"CopilotPeriodWeek":       "Woche",
			"CopilotPeriodMonth":      "Monat",
			"CopilotCurrentPeriod":    "Aktuelle Periode",
			"CopilotLastFetch":        "Letzter Abruf",
			"CopilotNextFetch":        "Nächster Abruf",
			"CopilotBillingPeriod":    "Abrechnungsperiode",
			"CopilotWarningNone":      "OK",
			"CopilotWarningNotice":    "Hinweis",
			"CopilotWarningWarning":   "Warnung",
			"CopilotWarningCritical":  "Kritisch",

			// Copilot settings + notifications
			"CopilotSettings":          "Copilot",
			"CopilotSettingsDesc":      "Abruf der GitHub-Copilot-Credits über die GitHub-CLI.",
			"CopilotEnabled":           "Abruf aktivieren",
			"CopilotFetchInterval":     "Abrufintervall",
			"CopilotFetchIntervalHint": "Wie oft die Credits abgerufen werden, z. B. 1h oder 30m.",
			"CopilotGHPath":            "Pfad zu gh",
			"CopilotDailyLimit":        "Tägliches Limit",
			"CopilotDailyLimitHint":    "Verbrauchte Credits pro Tag, ab denen eine Desktop-Benachrichtigung erscheint. 0 deaktiviert die Benachrichtigung.",
			"CopilotNotifyTitle":       "Copilot-Credit-Limit erreicht",
			"CopilotNotifyBody":        "{{.Consumed}} von {{.Limit}} Credits heute verbraucht.",
			"CopilotDailyLimitInvalid": "Das tägliche Limit muss eine nicht-negative Zahl sein.",

			// Copilot dashboard (hero, KPIs, chart, heatmap)
			"CopilotQuotaStatus":       "Kontingent-Status",
			"CopilotUsedLabel":         "verbraucht",
			"CopilotOrganization":      "Organisation",
			"CopilotResetIn":           "Reset in {{.Days}} Tagen",
			"CopilotResetOn":           "Zurücksetzung am {{.Date}}",
			"CopilotConsumedToday":     "Verbrauch heute",
			"CopilotAvgPerDay":         "Ø pro Tag",
			"CopilotThisMonth":         "Diesen Monat",
			"CopilotVsPrevDay":         "{{.Pct}}% ggü. Vortag",
			"CopilotAvgOverDays":       "Ø über {{.Days}} Tage im {{.Month}}",
			"CopilotOfEntitlement":     "{{.Pct}}% des Kontingents",
			"CopilotConsumptionPerDay": "Verbrauch pro Tag",
			"CopilotMonthOverview":     "Monatsübersicht",
			"CopilotIntensity":         "Verbrauchsintensität pro Kalendertag",
			"CopilotLess":              "weniger",
			"CopilotMore":              "mehr",
			"CopilotNoConsumption":     "kein Verbrauch",
			"CopilotUpdatedAgo":        "aktualisiert vor {{.Min}} min",
		},
		English: {
			// Navigation
			"NavToday":    "Today",
			"NavWeek":     "Week",
			"NavMonth":    "Month",
			"NavBooking":  "Booking",
			"NavClosures": "Closures",
			"NavCopilot":  "Copilot",
			"NavSettings": "Settings",

			// Page titles
			"PageToday":         "Today",
			"PageWeek":          "Week",
			"PageMonth":         "Month",
			"PageBooking":       "Booking",
			"PageClosures":      "Closures",
			"PageClosure":       "Closure",
			"PageClosureDetail": "Closure Detail",
			"PageCopilot":       "Copilot Credits",
			"PageSettings":      "Settings",
			"PageDay":           "Day",

			// Today / day view
			"TodayDate":              "Today - {{.Date}}",
			"Status":                 "Status",
			"CurrentBlockStarted":    "Current block started at {{.Time}} ({{.Minutes}} min so far)",
			"Worked":                 "Worked",
			"Pause":                  "Pause",
			"Booking":                "Booking",
			"Booked":                 "booked",
			"NotBooked":              "not booked",
			"ChangedAfterBooking":    "Warning: this day was changed after booking.",
			"WorkBlocks":             "Work blocks",
			"NoWorkBlocks":           "No work blocks recorded yet.",
			"Start":                  "Start",
			"End":                    "End",
			"Source":                 "Source",
			"SourceDetected":         "Automatic",
			"SourceManual":           "Manual",
			"Note":                   "Note",
			"Actions":                "Actions",
			"Ignore":                 "Ignore",
			"Unignore":               "Reset ignore",
			"Delete":                 "Delete",
			"Ignored":                "Ignored",
			"AddManualBlock":         "Add manual block",
			"Add":                    "Add",
			"Cancel":                 "Cancel",
			"SystemActive":           "System active",
			"TargetEightHours":       "Target 8h",
			"DailyTimeline":          "Daily timeline",
			"Running":                "Running",
			"Now":                    "Now",
			"Time":                   "Time",
			"Duration":               "Duration",
			"DailyProgress":          "Daily progress",
			"RemainingUntilTarget":   "{{.Duration}} remaining to target",
			"ThisWeek":               "This week",
			"BookingPeriod":          "Booking period",
			"Remaining":              "Remaining",
			"DaysRemainingShort":     "{{.Days}} days",
			"OpenBookingDaysWarning": "{{.Count}} days are not booked yet.",

			// Activity status labels (activity.ActivityState values)
			"StatusActive":    "Active",
			"StatusIdle":      "Idle",
			"StatusLocked":    "Locked",
			"StatusSuspended": "Suspended",
			"StatusUnknown":   "Unknown",

			// Week view
			"ISOWeekShort": "Week {{.Week}}",
			"WeekRange":    "Week {{.Start}} - {{.End}}",
			"CurrentWeek":  "Current week",
			"TotalTracked": "Total tracked",
			"BookedDays":   "Booked days",
			"UnbookedDays": "Unbooked days",
			"Day":          "Day",
			"Blocks":       "Blocks",
			"Changed":      "Changed",
			"Yes":          "yes",
			"No":           "no",

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
			"Differs":            "Differs",

			// Close preview
			"ClosePeriodPreview":  "Close Period Preview",
			"TrackedWorkdays":     "Tracked workdays",
			"TotalDuration":       "Total duration",
			"UnbookedDaysWarning": "Warning: {{.Count}} unbooked days.",
			"AcknowledgeUnbooked": "I acknowledge that unbooked days remain.",
			"CloseBookingPeriod":  "Close booking period",

			// Closure detail
			"Closure":            "Closure",
			"ClosedAt":           "Closed at",
			"CurrentDataDiffers": "Current data differs from this snapshot.",
			"DaySnapshot":        "Day snapshot",
			"TrackedDuration":    "Tracked duration",

			// Settings
			"Settings":                "Settings",
			"SettingsSections":        "Settings sections",
			"ActivitySettings":        "Activity",
			"ActivityDetection":       "Activity detection",
			"ActivityDetectionDesc":   "How Zeitspur detects work and pause times. GNOME/Mutter over D-Bus.",
			"ActivityMode":            "Activity mode",
			"ActivityModeIdleAndLock": "Idle and screen lock",
			"ActivityModeLockOnly":    "Screen lock only",
			"PollInterval":            "Poll interval",
			"PollIntervalHint":        "How often the idle status is queried, for example 5s or 10s.",
			"IdleThreshold":           "Idle threshold",
			"IdleThresholdModeNote":   "The idle threshold is only used in idle and screen lock mode. Screen lock and suspend always count as pauses.",
			"WorkdaySettings":         "Workdays",
			"WorkdaySettingsDesc":     "On which weekdays Zeitspur appears automatically on the Today page.",
			"LocaleSettings":          "Region & language",
			"LocaleSettingsDesc":      "Timezone and interface language.",
			"AppearanceSettings":      "Appearance",
			"AppearanceSettingsDesc":  "Layout and presentation of the interface.",
			"Navigation":              "Navigation",
			"NavigationHint":          "Position of the primary navigation. \u201cTop\u201d as a classic menu bar, \u201cLeft\u201d as a vertical sidebar.",
			"NavigationTop":           "Top",
			"NavigationSide":          "Left (sidebar)",
			"ServerSettings":          "Server",
			"ServerSettingsDesc":      "Where the local web interface is reachable. Localhost is recommended for security.",
			"ListenAddress":           "Listen address",
			"ListenAddressHint":       "Format host:port. Put a firewall or authentication in front of external access.",
			"Timezone":                "Timezone",
			"UseSystemTimezone":       "Use system timezone",
			"Language":                "Language",
			"TodayWeekdays":           "Weekdays in Today view",
			"Discard":                 "Discard",
			"Save":                    "Save",
			"Version":                 "Version",
			"SettingsPersisted":       "Settings are persisted to {{.Path}}",

			// Copilot credits
			"CopilotStatus":           "Current status",
			"CopilotPlan":             "Plan",
			"CopilotOrganizations":    "Organizations",
			"CopilotEntitlement":      "Entitlement",
			"CopilotRemaining":        "Remaining",
			"CopilotUsed":             "Used",
			"CopilotPercentRemaining": "Remaining %",
			"CopilotResetAt":          "Resets at",
			"CopilotBilling":          "Billing",
			"CopilotBillingToken":     "Token-based",
			"CopilotBillingSeat":      "Seat-based",
			"CopilotConsumption":      "Consumption",
			"CopilotConsumed":         "Consumed",
			"CopilotSamples":          "Samples",
			"CopilotTotalConsumed":    "Total consumed",
			"CopilotNoData":           "No Copilot data yet. The fetcher runs hourly; the first samples appear shortly.",
			"CopilotFetchError":       "Last fetch failed",

			// Copilot fetch error kinds (see internal/copilot.ErrorKind)
			"CopilotErrorUnavailable": "GitHub is temporarily unavailable (server error). The next fetch retries automatically.",
			"CopilotErrorNetwork":     "GitHub could not be reached. Check network or proxy; the next fetch retries automatically.",
			"CopilotErrorRateLimited": "GitHub API rate limit reached. The next fetch retries automatically.",
			"CopilotErrorAuth":        "GitHub CLI is not authenticated. Please run 'gh auth login'.",
			"CopilotErrorForbidden":   "GitHub denied access. Check token permissions or SSO authorization.",
			"CopilotErrorNoSeat":      "No Copilot access found for this account.",
			"CopilotErrorGhMissing":   "GitHub CLI (gh) not found. Check the path in the settings.",
			"CopilotErrorParse":       "Unexpected response from GitHub (no quota data).",
			"CopilotErrorUnknown":     "Fetch failed.",
			"CopilotPeriodWeek":       "Week",
			"CopilotPeriodMonth":      "Month",
			"CopilotCurrentPeriod":    "Current period",
			"CopilotLastFetch":        "Last fetch",
			"CopilotNextFetch":        "Next fetch",
			"CopilotBillingPeriod":    "Billing period",
			"CopilotWarningNone":      "OK",
			"CopilotWarningNotice":    "Notice",
			"CopilotWarningWarning":   "Warning",
			"CopilotWarningCritical":  "Critical",

			// Copilot settings + notifications
			"CopilotSettings":          "Copilot",
			"CopilotSettingsDesc":      "Fetching GitHub Copilot credits via the GitHub CLI.",
			"CopilotEnabled":           "Enable fetching",
			"CopilotFetchInterval":     "Fetch interval",
			"CopilotFetchIntervalHint": "How often credits are fetched, e.g. 1h or 30m.",
			"CopilotGHPath":            "Path to gh",
			"CopilotDailyLimit":        "Daily limit",
			"CopilotDailyLimitHint":    "Credits consumed per day at which a desktop notification appears. 0 disables notifications.",
			"CopilotNotifyTitle":       "Copilot credit limit reached",
			"CopilotNotifyBody":        "{{.Consumed}} of {{.Limit}} credits consumed today.",
			"CopilotDailyLimitInvalid": "The daily limit must be a non-negative number.",

			// Copilot dashboard (hero, KPIs, chart, heatmap)
			"CopilotQuotaStatus":       "Quota status",
			"CopilotUsedLabel":         "used",
			"CopilotOrganization":      "Organization",
			"CopilotResetIn":           "Reset in {{.Days}} days",
			"CopilotResetOn":           "Resets on {{.Date}}",
			"CopilotConsumedToday":     "Consumed today",
			"CopilotAvgPerDay":         "Avg per day",
			"CopilotThisMonth":         "This month",
			"CopilotVsPrevDay":         "{{.Pct}}% vs. yesterday",
			"CopilotAvgOverDays":       "Avg over {{.Days}} days in {{.Month}}",
			"CopilotOfEntitlement":     "{{.Pct}}% of entitlement",
			"CopilotConsumptionPerDay": "Consumption per day",
			"CopilotMonthOverview":     "Month overview",
			"CopilotIntensity":         "Consumption intensity per day",
			"CopilotLess":              "less",
			"CopilotMore":              "more",
			"CopilotNoConsumption":     "no consumption",
			"CopilotUpdatedAgo":        "updated {{.Min}} min ago",
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

// WarningLabel returns the localized label for a Copilot quota warning level.
// The lookup key is derived as "CopilotWarning"+PascalCase(level); unknown
// values fall back to the localized OK label (CopilotWarningNone).
func (c Catalog) WarningLabel(locale Locale, level string) string {
	key := "CopilotWarning" + pascalCase(level)
	if label := c.T(locale, key); label != key {
		return label
	}
	return c.T(locale, "CopilotWarningNone")
}

// ErrorLabel returns the localized description for a Copilot fetch error kind.
// The lookup key is derived as "CopilotError"+PascalCase(kind); unknown or
// empty values fall back to the generic CopilotErrorUnknown text.
func (c Catalog) ErrorLabel(locale Locale, kind string) string {
	key := "CopilotError" + pascalCase(kind)
	if label := c.T(locale, key); label != key {
		return label
	}
	return c.T(locale, "CopilotErrorUnknown")
}

// pascalCase converts a snake_case, kebab-case or space-separated identifier to
// PascalCase, e.g. "provider_error" becomes "ProviderError".
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
