package trips

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestDecimalKeepsScaleOnTheWire(t *testing.T) {
	cases := map[string]string{
		`350`:        `350`,
		`350.00`:     `350.00`,
		`"350"`:      `350`,
		`"350.0"`:    `350.0`,
		`10.586550`:  `10.586550`,
		`0.000000`:   `0.000000`,
		`-3.5`:       `-3.5`,
		`5000`:       `5000`,
		`1234567.89`: `1234567.89`,
	}
	for in, want := range cases {
		var d Decimal
		if err := json.Unmarshal([]byte(in), &d); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		got, _ := json.Marshal(d)
		if string(got) != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestDecimalRejectsGarbageAndKeepsNull(t *testing.T) {
	var d Decimal
	if err := json.Unmarshal([]byte(`"abc"`), &d); err == nil {
		t.Error("abc parsed")
	}
	var p *Decimal
	if err := json.Unmarshal([]byte(`null`), &p); err != nil || p != nil {
		t.Errorf("null: %v %v", p, err)
	}
}

func TestDecimalArithmeticFollowsCSharpScale(t *testing.T) {
	sum := DecimalFrom(MustDecimal("350.00").Add(MustDecimal("120.00").Decimal))
	if sum.String() != "470.00" {
		t.Errorf("sum %s", sum)
	}
	usd := DecimalFrom(MustDecimal("350").DivRound(MustDecimal("33.0608").Decimal, 28).RoundBank(6))
	if usd.String() != "10.586556" {
		t.Errorf("usd %s", usd)
	}
	half := DecimalFrom(decimal.RequireFromString("0.0000005").RoundBank(6))
	if half.String() != "0.000000" {
		t.Errorf("banker's rounding %s", half)
	}
}

func TestDecimalScanAndValue(t *testing.T) {
	var d Decimal
	if err := d.Scan([]byte("350.00")); err != nil {
		t.Fatal(err)
	}
	v, _ := d.Value()
	if v != "350.00" {
		t.Errorf("value %v", v)
	}
}

func TestDateWire(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`"2026-09-16"`), &d); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(d)
	if string(got) != `"2026-09-16"` {
		t.Errorf("got %s", got)
	}
	if d.AddDays(1).DaysSince(NewDate(2026, 9, 9)) != 8 {
		t.Errorf("days %d", d.AddDays(1).DaysSince(NewDate(2026, 9, 9)))
	}
	var scanned Date
	if err := scanned.Scan(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)); err != nil || scanned != d {
		t.Errorf("scan %v %v", scanned, err)
	}
}

func TestDateTimeInputOffsets(t *testing.T) {
	cases := map[string]string{
		`"2026-09-16T23:30:00+07:00"`: "2026-09-16T23:30:00+07:00",
		`"2026-09-16T10:00:00Z"`:      "2026-09-16T10:00:00Z",
		`"2026-09-16T00:00:00"`:       "2026-09-16T00:00:00Z",
		`"2026-09-16"`:                "2026-09-16T00:00:00Z",
	}
	for in, want := range cases {
		var d DateTimeInput
		if err := json.Unmarshal([]byte(in), &d); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := d.Format(time.RFC3339); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestStatusAndFloatWire(t *testing.T) {
	got, _ := json.Marshal(struct {
		S TripStatus `json:"s"`
	}{StatusActive})
	if string(got) != `{"s":"Active"}` {
		t.Errorf("status %s", got)
	}
	var req SetTimezoneRequest
	if err := json.Unmarshal([]byte(`{"Latitude":"13.75","longtitude":100.5}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Latitude != 13.75 || req.Longtitude != 100.5 {
		t.Errorf("floats %v", req)
	}
}
