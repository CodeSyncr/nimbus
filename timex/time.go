package timex

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Time is Nimbus' canonical timestamp type.
//
// Goals:
// - JSON round-trips as RFC3339/RFC3339Nano strings (never unix ints).
// - DB round-trips as a native datetime type (not 32-bit unix).
// - MySQL uses DATETIME(6) (Y2038-safe), not TIMESTAMP.
type Time struct {
	time.Time
}

func New(t time.Time) Time { return Time{Time: t} }

func (t Time) IsZero() bool { return t.Time.IsZero() }

// MarshalJSON encodes as RFC3339Nano string, or null for zero.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Time.Format(time.RFC3339Nano))
}

// UnmarshalJSON accepts RFC3339/RFC3339Nano (or null) and rejects numeric unix timestamps.
func (t *Time) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	if len(b) == 0 {
		return fmt.Errorf("timex.Time: empty JSON")
	}
	if b[0] != '"' {
		return fmt.Errorf("timex.Time: expected RFC3339 string or null")
	}

	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("timex.Time: invalid JSON string: %w", err)
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}

	parsed, err := parseTimeString(s)
	if err != nil {
		return err
	}

	// Guard against values outside typical SQL DATETIME ranges.
	// MySQL DATETIME supports years 1000-9999; we accept 0001-9999 to match Go's minimum,
	// but reject out-of-range years to avoid silent DB truncation/overflow.
	y := parsed.Year()
	if y < 1 || y > 9999 {
		return fmt.Errorf("timex.Time: year out of range: %d", y)
	}

	t.Time = parsed
	return nil
}

func parseTimeString(s string) (time.Time, error) {
	// RFC3339Nano first (strict superset for fractional seconds).
	if tt, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return tt, nil
	}
	if tt, err := time.Parse(time.RFC3339, s); err == nil {
		return tt, nil
	}
	// Common SQL formats (no timezone). Treat as local time.
	if tt, err := time.ParseInLocation("2006-01-02 15:04:05.999999", s, time.Local); err == nil {
		return tt, nil
	}
	if tt, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return tt, nil
	}
	if tt, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return tt, nil
	}
	return time.Time{}, fmt.Errorf("timex.Time: unsupported timestamp format %q", s)
}

// Value implements driver.Valuer for database writes.
func (t Time) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time, nil
}

// Scan implements sql.Scanner for database reads.
func (t *Time) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		t.Time = time.Time{}
		return nil
	case time.Time:
		t.Time = v
		return nil
	case []byte:
		tt, err := parseTimeString(string(v))
		if err != nil {
			return err
		}
		t.Time = tt
		return nil
	case string:
		tt, err := parseTimeString(v)
		if err != nil {
			return err
		}
		t.Time = tt
		return nil
	default:
		return fmt.Errorf("timex.Time: unsupported scan type %T", value)
	}
}

// GormDataType declares the generic data type for schema generation.
func (Time) GormDataType() string { return "datetime" }

// GormDBDataType selects driver-specific column types.
func (Time) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db == nil || db.Dialector == nil {
		return "DATETIME"
	}
	switch db.Dialector.Name() {
	case "mysql":
		return "DATETIME(6)"
	case "postgres", "pgx":
		return "TIMESTAMPTZ"
	default:
		return "DATETIME"
	}
}

