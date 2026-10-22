package trips

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

type Decimal struct {
	decimal.Decimal
}

func DecimalFrom(d decimal.Decimal) Decimal { return Decimal{d} }

func ParseDecimal(s string) (Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Decimal{}, err
	}
	return Decimal{d}, nil
}

func MustDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Decimal) String() string {
	if exp := d.Exponent(); exp < 0 {
		return d.StringFixed(-exp)
	}
	return d.Decimal.String()
}

func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *Decimal) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	text := string(data)
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		unquoted, err := strconv.Unquote(text)
		if err != nil {
			return err
		}
		text = unquoted
	}
	parsed, err := ParseDecimal(text)
	if err != nil {
		return fmt.Errorf("invalid decimal %s", data)
	}
	*d = parsed
	return nil
}

func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}

func (d *Decimal) Scan(value any) error {
	switch v := value.(type) {
	case string:
		parsed, err := ParseDecimal(v)
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		parsed, err := ParseDecimal(string(v))
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	}
	return d.Decimal.Scan(value)
}

type Date struct {
	year  int
	month time.Month
	day   int
}

const dateLayout = "2006-01-02"

func NewDate(year int, month time.Month, day int) Date {
	return DateOf(time.Date(year, month, day, 0, 0, 0, 0, time.UTC))
}

func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{year: y, month: m, day: d}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, err
	}
	return DateOf(t), nil
}

func (d Date) IsZero() bool { return d == Date{} }

func (d Date) Time() time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
}

func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n)) }

func (d Date) DaysSince(other Date) int {
	return int(d.Time().Sub(other.Time()) / (24 * time.Hour))
}

func (d Date) Before(other Date) bool { return d.Time().Before(other.Time()) }
func (d Date) After(other Date) bool  { return d.Time().After(other.Time()) }

func (d Date) String() string { return d.Time().Format(dateLayout) }

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Date) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Date) Value() (driver.Value, error) { return d.String(), nil }

func (d *Date) Scan(value any) error {
	switch v := value.(type) {
	case time.Time:

		*d = DateOf(v)
		return nil
	case string:
		parsed, err := ParseDate(v)
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		parsed, err := ParseDate(string(v))
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	}
	return fmt.Errorf("trips: cannot scan %T into Date", value)
}

type DateTimeInput struct {
	time.Time
}

var dateTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04",
	dateLayout,
}

func ParseDateTimeInput(s string) (DateTimeInput, error) {
	for i, layout := range dateTimeLayouts {
		var (
			t   time.Time
			err error
		)
		if i == 0 {
			t, err = time.Parse(layout, s)
		} else {
			t, err = time.ParseInLocation(layout, s, time.UTC)
		}
		if err == nil {
			return DateTimeInput{t}, nil
		}
	}
	return DateTimeInput{}, fmt.Errorf("invalid date-time %q", s)
}

func (d *DateTimeInput) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseDateTimeInput(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

type Float float64

func (f *Float) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	text := string(data)
	if len(data) >= 2 && data[0] == '"' {
		unquoted, err := strconv.Unquote(text)
		if err != nil {
			return err
		}
		text = unquoted
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("invalid number %s", data)
	}
	*f = Float(parsed)
	return nil
}
