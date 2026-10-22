package trips

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const (
	msgStartNotPlanned = "Попытка активировать уже активную или законченную поездку"
	msgEndCompleted    = "Попытка закончить уже законченную поездку"
)

func (t *Trip) Start(on *Date, today Date) error {
	if t.Status != StatusPlanned {
		return Conflict(msgStartNotPlanned)
	}
	start := today
	if on != nil {
		start = *on
	}
	t.Status = StatusActive
	t.StartDate = &start
	return nil
}

func (t *Trip) End(on *Date, today Date) error {
	if t.Status == StatusCompleted {
		return Conflict(msgEndCompleted)
	}
	end := today
	if on != nil {
		end = *on
	}
	if t.StartDate != nil && end.Before(*t.StartDate) {
		end = *t.StartDate
	}
	t.Status = StatusCompleted
	t.EndDate = &end
	return nil
}

func (t *Trip) SetCountry(country Country) {
	code := country.Code
	t.CountryCode = &code
	name := country.NameRu
	t.CountryName = &name
	if t.CurrencyCode == nil {
		currency := country.DefaultCurrencyCode
		t.CurrencyCode = &currency
	}
	if isBlank(t.Timezone) {
		_ = t.SetTimeZone(country.DefaultTimezone)
	}
}

func (t *Trip) SetTimeZone(timezone string) error {
	if strings.TrimSpace(timezone) == "" {
		return Validation("Часовой пояс не может быть пустым")
	}
	t.Timezone = &timezone
	return nil
}

func (t *Trip) DurationDays(today Date) int {
	if t.StartDate == nil {
		return 0
	}
	end := today
	if t.EndDate != nil {
		end = *t.EndDate
	}
	if end.Before(*t.StartDate) {
		return 0
	}
	return end.DaysSince(*t.StartDate) + 1
}

func LocalDate(moment time.Time, timezone *string) (Date, error) {
	if isBlank(timezone) {
		return DateOf(moment), nil
	}
	loc, err := loadLocation(*timezone)
	if err != nil {
		return Date{}, err
	}
	return DateOf(moment.In(loc)), nil
}

var locations sync.Map

func loadLocation(name string) (*time.Location, error) {
	if cached, ok := locations.Load(name); ok {
		return cached.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locations.Store(name, loc)
	return loc, nil
}

func IsSimilarTrip(active Trip, country *Country, city *string) bool {
	countryCompatible := isBlank(active.CountryCode) ||
		country == nil ||
		strings.EqualFold(*active.CountryCode, country.Code)
	cityCompatible := isBlank(active.City) ||
		isBlank(city) ||
		strings.EqualFold(strings.TrimSpace(*active.City), strings.TrimSpace(*city))
	return countryCompatible && cityCompatible
}

func FillBlanks(t *Trip, payload SyncTripCreatePayload, country *Country) {
	if isBlank(t.CurrencyCode) && !isBlank(payload.CurrencyCode) {
		upper := strings.ToUpper(*payload.CurrencyCode)
		t.CurrencyCode = &upper
	}
	if isBlank(t.CountryCode) && country != nil {
		t.SetCountry(*country)
	}
	if isBlank(t.City) && !isBlank(payload.City) {
		city := *payload.City
		t.City = &city
	}
	if t.Budget == nil && payload.Budget != nil {
		budget := *payload.Budget
		t.Budget = &budget
	}
}

func FormatUTCOffset(offset time.Duration) string {
	hours := math.Round(offset.Hours()*100) / 100
	sign := ""
	if offset >= 0 {
		sign = "+"
	}
	if hours == 0 {
		hours = 0
	}
	return "UTC" + sign + strconv.FormatFloat(hours, 'f', -1, 64)
}

func isBlank(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

func equalFoldNullable(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return strings.EqualFold(*a, *b)
}
