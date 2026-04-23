package timex

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTime_JSON_RFC3339String(t *testing.T) {
	tt := New(time.Date(2040, 1, 2, 3, 4, 5, 123456000, time.UTC))

	b, err := json.Marshal(tt)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[0] != '"' {
		t.Fatalf("expected JSON string, got %s", string(b))
	}

	var out Time
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Equal(tt.Time) {
		t.Fatalf("roundtrip mismatch: got %v want %v", out.Time, tt.Time)
	}
}

func TestTime_UnmarshalJSON_RejectsUnixNumber(t *testing.T) {
	var out Time
	if err := json.Unmarshal([]byte(`2147483648`), &out); err == nil {
		t.Fatalf("expected error for numeric unix timestamp")
	}
}

func TestTime_UnmarshalJSON_Null(t *testing.T) {
	var out Time
	if err := json.Unmarshal([]byte(`null`), &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsZero() {
		t.Fatalf("expected zero, got %v", out.Time)
	}
}

func TestTime_UnmarshalJSON_RejectsYear10000(t *testing.T) {
	var out Time
	err := json.Unmarshal([]byte(`"10000-01-01T00:00:00Z"`), &out)
	if err == nil {
		t.Fatal("expected error for year out of SQL-friendly range")
	}
}

