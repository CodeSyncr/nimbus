package timex

import (
	"fmt"
	"time"
)

// NimbusTime is a fluent wrapper around time.Time mimicking Carbon/Luxon.
type NimbusTime struct {
	t time.Time
}

// Now returns a new NimbusTime instance for the current time.
func Now() NimbusTime {
	return NimbusTime{t: time.Now()}
}

// Parse parses a formatted string and returns a NimbusTime instance.
// Defaults to RFC3339, but can be expanded for more formats.
func Parse(s string) (NimbusTime, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", s)
	}
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
	}
	return NimbusTime{t: t}, err
}

// FromTime wraps an existing time.Time into a NimbusTime.
func FromTime(t time.Time) NimbusTime {
	return NimbusTime{t: t}
}

// =========================================================================
// Manipulation Methods (Chainable)
// =========================================================================

func (n NimbusTime) AddDays(days int) NimbusTime {
	return FromTime(n.t.AddDate(0, 0, days))
}

func (n NimbusTime) SubDays(days int) NimbusTime {
	return n.AddDays(-days)
}

func (n NimbusTime) AddHours(hours int) NimbusTime {
	return FromTime(n.t.Add(time.Duration(hours) * time.Hour))
}

func (n NimbusTime) SubHours(hours int) NimbusTime {
	return n.AddHours(-hours)
}

func (n NimbusTime) AddMinutes(minutes int) NimbusTime {
	return FromTime(n.t.Add(time.Duration(minutes) * time.Minute))
}

func (n NimbusTime) SubMinutes(minutes int) NimbusTime {
	return n.AddMinutes(-minutes)
}

func (n NimbusTime) AddMonths(months int) NimbusTime {
	return FromTime(n.t.AddDate(0, months, 0))
}

func (n NimbusTime) SubMonths(months int) NimbusTime {
	return n.AddMonths(-months)
}

func (n NimbusTime) AddYears(years int) NimbusTime {
	return FromTime(n.t.AddDate(years, 0, 0))
}

func (n NimbusTime) SubYears(years int) NimbusTime {
	return n.AddYears(-years)
}

func (n NimbusTime) StartOfDay() NimbusTime {
	y, m, d := n.t.Date()
	return FromTime(time.Date(y, m, d, 0, 0, 0, 0, n.t.Location()))
}

func (n NimbusTime) EndOfDay() NimbusTime {
	y, m, d := n.t.Date()
	return FromTime(time.Date(y, m, d, 23, 59, 59, 999999999, n.t.Location()))
}

func (n NimbusTime) StartOfWeek() NimbusTime {
	// Assuming Monday as the start of the week
	offset := int(time.Monday - n.t.Weekday())
	if offset > 0 {
		offset = -6
	}
	return n.AddDays(offset).StartOfDay()
}

func (n NimbusTime) EndOfWeek() NimbusTime {
	return n.StartOfWeek().AddDays(6).EndOfDay()
}

func (n NimbusTime) StartOfMonth() NimbusTime {
	y, m, _ := n.t.Date()
	return FromTime(time.Date(y, m, 1, 0, 0, 0, 0, n.t.Location()))
}

func (n NimbusTime) EndOfMonth() NimbusTime {
	return n.StartOfMonth().AddMonths(1).SubDays(1).EndOfDay()
}

func (n NimbusTime) StartOfYear() NimbusTime {
	y, _, _ := n.t.Date()
	return FromTime(time.Date(y, time.January, 1, 0, 0, 0, 0, n.t.Location()))
}

func (n NimbusTime) EndOfYear() NimbusTime {
	y, _, _ := n.t.Date()
	return FromTime(time.Date(y, time.December, 31, 23, 59, 59, 999999999, n.t.Location()))
}

// =========================================================================
// Comparison Methods (Terminal)
// =========================================================================

func (n NimbusTime) IsBefore(other NimbusTime) bool {
	return n.t.Before(other.t)
}

func (n NimbusTime) IsAfter(other NimbusTime) bool {
	return n.t.After(other.t)
}

func (n NimbusTime) IsSame(other NimbusTime) bool {
	return n.t.Equal(other.t)
}

func (n NimbusTime) IsBetween(start, end NimbusTime) bool {
	return n.IsAfter(start) && n.IsBefore(end)
}

func (n NimbusTime) IsToday() bool {
	now := time.Now()
	y1, m1, d1 := n.t.Date()
	y2, m2, d2 := now.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func (n NimbusTime) IsPast() bool {
	return n.t.Before(time.Now())
}

func (n NimbusTime) IsFuture() bool {
	return n.t.After(time.Now())
}

func (n NimbusTime) IsWeekend() bool {
	w := n.t.Weekday()
	return w == time.Saturday || w == time.Sunday
}

func (n NimbusTime) IsWeekday() bool {
	return !n.IsWeekend()
}

// =========================================================================
// Output Methods (Terminal)
// =========================================================================

func (n NimbusTime) Format(layout string) string {
	return n.t.Format(layout)
}

func (n NimbusTime) ToDateString() string {
	return n.t.Format("2006-01-02")
}

func (n NimbusTime) ToTimeString() string {
	return n.t.Format("15:04:05")
}

func (n NimbusTime) ToDateTimeString() string {
	return n.t.Format("2006-01-02 15:04:05")
}

func (n NimbusTime) ToISO() string {
	return n.t.Format(time.RFC3339)
}

func (n NimbusTime) DiffForHumans() string {
	now := time.Now()
	diff := n.t.Sub(now)
	isFuture := diff > 0
	if !isFuture {
		diff = -diff
	}

	var val int
	var unit string

	if diff.Hours() >= 24*365 {
		val = int(diff.Hours() / (24 * 365))
		unit = "year"
	} else if diff.Hours() >= 24*30 {
		val = int(diff.Hours() / (24 * 30))
		unit = "month"
	} else if diff.Hours() >= 24 {
		val = int(diff.Hours() / 24)
		unit = "day"
	} else if diff.Hours() >= 1 {
		val = int(diff.Hours())
		unit = "hour"
	} else if diff.Minutes() >= 1 {
		val = int(diff.Minutes())
		unit = "minute"
	} else {
		val = int(diff.Seconds())
		unit = "second"
	}

	if val != 1 {
		unit += "s"
	}

	if val == 0 && unit == "seconds" {
		return "just now"
	}

	if isFuture {
		return fmt.Sprintf("in %d %s", val, unit)
	}
	return fmt.Sprintf("%d %s ago", val, unit)
}

func (n NimbusTime) DiffInDays(other NimbusTime) int {
	diff := n.t.Sub(other.t).Hours() / 24
	return int(diff)
}

func (n NimbusTime) DiffInHours(other NimbusTime) int {
	diff := n.t.Sub(other.t).Hours()
	return int(diff)
}

func (n NimbusTime) Unix() int64 {
	return n.t.Unix()
}

func (n NimbusTime) Time() time.Time {
	return n.t
}
