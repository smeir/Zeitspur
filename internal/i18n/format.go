package i18n

import (
	"time"
)

var (
	germanWeekdays = []string{
		"Sonntag", "Montag", "Dienstag", "Mittwoch",
		"Donnerstag", "Freitag", "Samstag",
	}
	germanWeekdaysShort = []string{
		"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa",
	}
	germanMonths = []string{
		"", "Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember",
	}
	germanMonthsShort = []string{
		"", "Jan", "Feb", "Mär", "Apr", "Mai", "Jun",
		"Jul", "Aug", "Sep", "Okt", "Nov", "Dez",
	}
)

// DateShort returns a short locale-aware date string.
func DateShort(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return t.Format("02.01.2006")
	default:
		return t.Format("2006-01-02")
	}
}

// DateLong returns a date string including the localized weekday.
func DateLong(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanWeekdays[t.Weekday()] + ", " + t.Format("02.01.2006")
	default:
		return t.Format("Monday, 2006-01-02")
	}
}

// DateTime returns a locale-aware date and time string.
func DateTime(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return t.Format("02.01.2006 15:04")
	default:
		return t.Format("2006-01-02 15:04")
	}
}

// MonthYear returns a localized month and year label.
func MonthYear(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanMonths[t.Month()] + " " + t.Format("2006")
	default:
		return t.Format("January 2006")
	}
}

// WeekdayName returns the localized full weekday name.
func WeekdayName(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanWeekdays[t.Weekday()]
	default:
		return t.Format("Monday")
	}
}

// WeekdayNameFor returns the localized full weekday name for a time.Weekday.
func WeekdayNameFor(wd time.Weekday, locale Locale) string {
	switch locale {
	case German:
		return germanWeekdays[wd]
	default:
		return wd.String()
	}
}

// WeekdayShort returns the localized abbreviated weekday name.
func WeekdayShort(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanWeekdaysShort[t.Weekday()]
	default:
		return t.Format("Mon")
	}
}

// WeekdayShortFor returns the localized abbreviated weekday name for a time.Weekday.
func WeekdayShortFor(wd time.Weekday, locale Locale) string {
	switch locale {
	case German:
		return germanWeekdaysShort[wd]
	default:
		return wd.String()[:3]
	}
}

// MonthName returns the localized full month name.
func MonthName(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanMonths[t.Month()]
	default:
		return t.Format("January")
	}
}

// MonthShort returns the localized abbreviated month name.
func MonthShort(t time.Time, locale Locale) string {
	switch locale {
	case German:
		return germanMonthsShort[t.Month()]
	default:
		return t.Format("Jan")
	}
}
