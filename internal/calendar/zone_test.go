package calendar

import (
	"testing"
	"time"
)

func TestWireTimeReadsAllDayDateInUTC(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	stored := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC).In(newYork)
	if got, want := wireTime(stored, true), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("all-day: %v, want %v", got, want)
	}
	if got := wireTime(stored, false); !got.Equal(stored) || got.Location() != time.UTC {
		t.Fatalf("timed: %v, want the same instant in UTC", got)
	}
}

func TestHumanLead(t *testing.T) {
	for lead, want := range map[time.Duration]string{
		15 * time.Minute: "15 мин.",
		time.Hour:        "1 ч.",
		24 * time.Hour:   "сутки",
		48 * time.Hour:   "2 дн.",
	} {
		if got := humanLead(lead); got != want {
			t.Errorf("humanLead(%v) = %q, want %q", lead, got, want)
		}
	}
}

func TestValidZone(t *testing.T) {
	for name, want := range map[string]bool{"Asia/Bangkok": true, "UTC": true, "Local": false, "": false, "Mars/Base": false} {
		if got := validZone(name); got != want {
			t.Errorf("validZone(%q) = %v, want %v", name, got, want)
		}
	}
}
