package tz

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"lifelog-api/internal/trips"

	"github.com/ringsaturn/tzf"
)

type Lookup struct {
	finder tzf.F
}

func New() (*Lookup, error) {
	finder, err := tzf.NewDefaultFinder()
	if err != nil {
		return nil, fmt.Errorf("tz: load boundaries: %w", err)
	}
	return &Lookup{finder: finder}, nil
}

var _ trips.TimezoneLookup = (*Lookup)(nil)

func (l *Lookup) Lookup(latitude, longitude float64) (string, error) {
	if math.IsNaN(longitude) || longitude < -180 || longitude > 180 {
		return nauticalZone(longitude), nil
	}
	switch {
	case math.IsNaN(latitude) || latitude < -90:
		latitude = -90
	case latitude > 90:
		latitude = 90
	}

	name := l.finder.GetTimezoneName(longitude, latitude)
	if name == "" || strings.HasPrefix(name, "Etc/") {
		return nauticalZone(longitude), nil
	}
	return name, nil
}

func nauticalZone(longitude float64) string {
	abs := math.Abs(longitude)

	if math.IsNaN(abs) || abs <= 7.5 {
		return "UTC"
	}
	rest := abs - 7.5
	bands := rest / 15
	if math.Mod(rest, 15) > 0 {
		bands++
	}

	hours := int64(math.MaxInt32)
	if f := math.Floor(bands); f < math.MaxInt32 {
		hours = int64(f)
	}
	sign := "-"
	if longitude < 0 {
		sign = "+"
	}
	return "Etc/GMT" + sign + strconv.FormatInt(hours, 10)
}

func (l *Lookup) BaseOffset(timezone string, year int) (time.Duration, error) {
	if timezone == "" || timezone == "Local" {
		return 0, errors.New("tz: time zone not found: " + strconv.Quote(timezone))
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, fmt.Errorf("tz: time zone not found: %w", err)
	}

	start := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	end := time.Date(year+1, time.January, 1, 0, 0, 0, 0, loc)
	_, minOffset := start.Zone()

	for t, n := start, 0; t.Before(end) && n < 64; n++ {
		if _, offset := t.Zone(); offset < minOffset {
			minOffset = offset
		}
		_, next := t.ZoneBounds()
		if next.IsZero() || !next.After(t) {
			break
		}
		t = next.In(loc)
	}
	return time.Duration(minOffset) * time.Second, nil
}
