package tz

import (
	"math"
	"sync"
	"testing"
	"time"
)

var (
	sharedOnce   sync.Once
	sharedLookup *Lookup
	sharedErr    error
)

func lookup(t *testing.T) *Lookup {
	t.Helper()
	sharedOnce.Do(func() { sharedLookup, sharedErr = New() })
	if sharedErr != nil {
		t.Fatalf("New: %v", sharedErr)
	}
	return sharedLookup
}

const noZone = math.MinInt

var geoTimeZoneCases = []struct {
	name          string
	lat, lon      float64
	zone          string
	offsetMinutes int
}{
	{"Bangkok", 13.7563, 100.5018, "Asia/Bangkok", 420},
	{"Phuket", 7.8804, 98.3923, "Asia/Bangkok", 420},
	{"Moscow", 55.7558, 37.6173, "Europe/Moscow", 180},
	{"Kolkata", 22.5726, 88.3639, "Asia/Kolkata", 330},
	{"Kathmandu", 27.7172, 85.324, "Asia/Kathmandu", 345},
	{"New York", 40.7128, -74.006, "America/New_York", -300},
	{"mid Atlantic", 30, -40, "Etc/GMT+3", -180},
	{"mid Pacific", 0, -150, "Etc/GMT+10", -600},
	{"null island", 0, 0, "UTC", 0},
	{"Indian Ocean", -10, 80, "Etc/GMT-5", 300},
	{"dateline east", 0, 179.9, "Etc/GMT-12", 720},
	{"dateline west", 0, -179.9, "Etc/GMT+12", -720},
	{"lon 180", 0, 180, "Etc/GMT-12", 720},
	{"lon -180", 0, -180, "Etc/GMT+12", -720},
	{"north pole", 90, 0, "UTC", 0},
	{"south pole", -90, 0, "Antarctica/McMurdo", 720},
	{"Dublin", 53.3498, -6.2603, "Europe/Dublin", 0},
	{"Casablanca", 33.5731, -7.5898, "Africa/Casablanca", 0},
	{"Adelaide", -34.9285, 138.6007, "Australia/Adelaide", 570},
	{"Chatham", -43.95, -176.56, "Pacific/Chatham", 765},
	{"Lord Howe", -31.5553, 159.0821, "Australia/Lord_Howe", 630},
	{"Tehran", 35.6892, 51.389, "Asia/Tehran", 210},
	{"St Johns", 47.5615, -52.7126, "America/St_Johns", -210},
	{"Kiritimati", 1.8721, -157.4278, "Pacific/Kiritimati", 840},
	{"Almaty", 43.2389, 76.8897, "Asia/Almaty", 300},
	{"Apia", -13.8333, -171.7667, "Pacific/Apia", 780},
	{"Santiago", -33.4489, -70.6693, "America/Santiago", -240},
	{"Marquesas", -8.9109, -140.0999, "Pacific/Marquesas", -570},
	{"Eucla", -31.677, 128.889, "Australia/Eucla", 525},
	{"Svalbard", 78.22, 15.65, "Arctic/Longyearbyen", 60},
	{"Alert", 82.5, -62.3, "America/Iqaluit", -300},
	{"Denpasar", -8.65, 115.2167, "Asia/Makassar", 480},
	{"San Diego", 32.7157, -117.1611, "America/Los_Angeles", -480},
	{"Tijuana", 32.5149, -117.0382, "America/Tijuana", -480},
	{"sea lon 7.5", 0, 7.5, "UTC", 0},
	{"sea lon -7.5", 0, -7.5, "UTC", 0},
	{"sea lon 172.5", 0, 172.5, "Etc/GMT-11", 660},
	{"sea lon -172.5", 0, -172.5, "Etc/GMT+11", -660},
	{"sea lon 22.5", -40, 22.5, "Etc/GMT-1", 60},
	{"sea lon 37.5", -40, 37.5, "Etc/GMT-2", 120},
	{"sea lon -22.5", -40, -22.5, "Etc/GMT+1", -60},
	{"sea lon 52.4", -40, 52.4, "Etc/GMT-3", 180},
	{"lat 91", 91, 0, "UTC", 0},
	{"lat 95 lon 100.5", 95, 100.5, "Etc/GMT-7", 420},
	{"lat -95 lon 100.5", -95, 100.5, "Antarctica/McMurdo", 720},
	{"lat -1000", -1000, 0, "Antarctica/McMurdo", 720},
	{"lon 181", 0, 181, "Etc/GMT-12", 720},
	{"lon -180.5", 0, -180.5, "Etc/GMT+12", -720},
	{"lon 200", 0, 200, "Etc/GMT-13", 780},
	{"lon -200", 0, -200, "Etc/GMT+13", noZone},
	{"lon 460.5", 13.75, 460.5, "Etc/GMT-31", noZone},
	{"lat NaN", math.NaN(), 0, "Antarctica/McMurdo", 720},
	{"lon NaN", 0, math.NaN(), "UTC", 0},
	{"lon -Inf", 0, math.Inf(-1), "Etc/GMT+2147483647", noZone},
	{"lat +Inf", math.Inf(1), 0, "UTC", 0},
}

func TestLookupMatchesGeoTimeZone(t *testing.T) {
	l := lookup(t)
	for _, tc := range geoTimeZoneCases {
		t.Run(tc.name, func(t *testing.T) {
			zone, err := l.Lookup(tc.lat, tc.lon)
			if err != nil {
				t.Fatalf("Lookup(%v, %v): %v", tc.lat, tc.lon, err)
			}
			if zone != tc.zone {
				t.Fatalf("Lookup(%v, %v) = %q, want %q", tc.lat, tc.lon, zone, tc.zone)
			}

			offset, err := l.BaseOffset(zone, 2026)
			if tc.offsetMinutes == noZone {
				if err == nil {
					t.Fatalf("BaseOffset(%q) = %v, want an error", zone, offset)
				}
				return
			}
			if err != nil {
				t.Fatalf("BaseOffset(%q): %v", zone, err)
			}
			if want := time.Duration(tc.offsetMinutes) * time.Minute; offset != want {
				t.Fatalf("BaseOffset(%q) = %v, want %v", zone, offset, want)
			}
		})
	}
}

func TestBaseOffsetFollowsTheYear(t *testing.T) {
	l := lookup(t)
	cases := []struct {
		zone string
		year int
		want time.Duration
	}{
		{"Asia/Almaty", 2023, 6 * time.Hour},
		{"Asia/Almaty", 2024, 5 * time.Hour},

		{"Australia/Sydney", 2026, 10 * time.Hour},
		{"America/Sao_Paulo", 2026, -3 * time.Hour},
		{"Europe/London", 2026, 0},
	}
	for _, tc := range cases {
		got, err := l.BaseOffset(tc.zone, tc.year)
		if err != nil {
			t.Fatalf("BaseOffset(%q, %d): %v", tc.zone, tc.year, err)
		}
		if got != tc.want {
			t.Errorf("BaseOffset(%q, %d) = %v, want %v", tc.zone, tc.year, got, tc.want)
		}
	}
}

func TestBaseOffsetUnknownZone(t *testing.T) {
	l := lookup(t)
	for _, zone := range []string{"", "Local", "Nowhere/City", "Etc/GMT+13", "../../etc/passwd"} {
		if got, err := l.BaseOffset(zone, 2026); err == nil {
			t.Errorf("BaseOffset(%q) = %v, want an error", zone, got)
		}
	}
}

func TestNauticalZone(t *testing.T) {
	cases := []struct {
		lon  float64
		want string
	}{
		{0, "UTC"},
		{7.5, "UTC"},
		{-7.5, "UTC"},
		{7.500001, "Etc/GMT-1"},
		{22.5, "Etc/GMT-1"},
		{-22.5, "Etc/GMT+1"},
		{22.6, "Etc/GMT-2"},
		{180, "Etc/GMT-12"},
		{360, "Etc/GMT-24"},
		{540, "Etc/GMT-36"},
		{math.NaN(), "UTC"},
		{math.Inf(1), "Etc/GMT-2147483647"},
		{math.Inf(-1), "Etc/GMT+2147483647"},
	}
	for _, tc := range cases {
		if got := nauticalZone(tc.lon); got != tc.want {
			t.Errorf("nauticalZone(%v) = %q, want %q", tc.lon, got, tc.want)
		}
	}
}
