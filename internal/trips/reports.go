package trips

import (
	"context"
	"sort"
	"strings"

	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

type ReportService struct {
	store Store
	clock Clock
}

func NewReportService(store Store, clock Clock) *ReportService {
	return &ReportService{store: store, clock: clock}
}

func (s *ReportService) Report(ctx context.Context, actor Actor, r DateRange) (ReportResponse, error) {
	today := DateOf(s.clock.Now())
	from, to := today, today
	if r.From != nil {
		from = *r.From
	}
	if r.To != nil {
		to = *r.To
	}

	rows, err := s.store.ReportRows(ctx, actor.UserID, DateRange{From: &from, To: &to})
	if err != nil {
		return ReportResponse{}, err
	}

	categoryIndex := map[string]int{}
	result := []ReportCategory{}
	totals := currencySums{}
	for _, row := range rows {
		at, ok := categoryIndex[row.CategoryName]
		if !ok {
			at = len(result)
			categoryIndex[row.CategoryName] = at
			result = append(result, ReportCategory{CategoryName: row.CategoryName, Amounts: []CurrencyAmount{}})
		}
		result[at].Amounts = addAmount(result[at].Amounts, row.CurrencyCode, row.Amount)
		totals.add(row.CurrencyCode, row.Amount)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return cultureLess(result[i].CategoryName, result[j].CategoryName)
	})
	totalAmounts := totals.list()
	sort.SliceStable(totalAmounts, func(i, j int) bool {
		return totalAmounts[i].Currency < totalAmounts[j].Currency
	})

	return ReportResponse{Result: result, TotalAmounts: totalAmounts, From: from, To: to}, nil
}

type ReportBuilderQuery struct {
	Range           DateRange
	Dimensions      string
	IncludeExpenses bool
}

const (
	DimensionTrip     = "trip"
	DimensionCity     = "city"
	DimensionCategory = "category"
)

func (s *ReportService) Builder(ctx context.Context, actor Actor, q ReportBuilderQuery) (ReportBuilderResponse, error) {
	dimensions := ParseDimensions(q.Dimensions)
	rows, err := s.store.BuilderRows(ctx, actor.UserID, q.Range)
	if err != nil {
		return ReportBuilderResponse{}, err
	}

	selected := map[string]bool{}
	for _, d := range dimensions {
		selected[d] = true
	}

	type groupKey struct {
		tripID       uuid.UUID
		tripName     string
		hasCity      bool
		city         string
		categoryID   int64
		categoryName string
	}
	keyOf := func(row *ExpenseDetail) groupKey {
		var key groupKey
		if selected[DimensionTrip] {
			key.tripID, key.tripName = row.TripID, row.TripName
		}
		if selected[DimensionCity] && !isBlank(row.TripCity) {
			key.hasCity, key.city = true, *row.TripCity
		}
		if selected[DimensionCategory] {
			key.categoryID, key.categoryName = row.CategoryID, row.CategoryName
		}
		return key
	}

	type bucket struct {
		key  groupKey
		rows []*ExpenseDetail
	}
	index := map[groupKey]int{}
	var buckets []bucket
	total := Decimal{}
	for i := range rows {
		row := &rows[i]
		total = DecimalFrom(total.Add(row.AmountUSD.Decimal))
		key := keyOf(row)
		at, ok := index[key]
		if !ok {
			at = len(buckets)
			index[key] = at
			buckets = append(buckets, bucket{key: key})
		}
		buckets[at].rows = append(buckets[at].rows, row)
	}

	groups := make([]ReportGroup, 0, len(buckets))
	for _, b := range buckets {
		group := ReportGroup{Count: len(b.rows), Amounts: []CurrencyAmount{}, Expenses: []ReportExpense{}}
		sums := currencySums{}
		for _, row := range b.rows {
			group.TotalUSD = DecimalFrom(group.TotalUSD.Add(row.AmountUSD.Decimal))
			sums.add(row.CurrencyCode, row.Amount)
		}
		group.Amounts = sums.list()
		sort.SliceStable(group.Amounts, func(i, j int) bool {
			return group.Amounts[i].Amount.Cmp(group.Amounts[j].Amount.Decimal) > 0
		})

		if selected[DimensionTrip] {
			tripID, tripName := b.key.tripID, b.key.tripName
			group.TripID, group.TripName = &tripID, &tripName
		}
		if selected[DimensionCity] && b.key.hasCity {
			city := b.key.city
			group.City = &city
		}
		if selected[DimensionCategory] {
			categoryID, categoryName := b.key.categoryID, b.key.categoryName
			group.CategoryID, group.CategoryName = &categoryID, &categoryName
		}

		if q.IncludeExpenses {
			sorted := append([]*ExpenseDetail(nil), b.rows...)
			sort.SliceStable(sorted, func(i, j int) bool {
				if sorted[i].OccurredAt != sorted[j].OccurredAt {
					return sorted[i].OccurredAt.After(sorted[j].OccurredAt)
				}
				return sorted[i].ID > sorted[j].ID
			})
			for _, row := range sorted {
				city := ""
				if row.TripCity != nil {
					city = *row.TripCity
				}
				group.Expenses = append(group.Expenses, ReportExpense{
					ID:           row.ID,
					OccurredAt:   row.OccurredAt,
					Description:  row.Description,
					Amount:       row.Amount,
					CurrencyCode: row.CurrencyCode,
					AmountUSD:    row.AmountUSD,
					CategoryName: row.CategoryName,
					TripName:     row.TripName,
					City:         city,
				})
			}
		}
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].TotalUSD.Cmp(groups[j].TotalUSD.Decimal) > 0
	})

	truncated := false
	if q.IncludeExpenses {
		returned := 0
		for _, g := range groups {
			returned += len(g.Expenses)
		}
		if returned > BuilderExpenseLimit {
			truncated = true
			budget := BuilderExpenseLimit
			for i := range groups {
				if len(groups[i].Expenses) <= budget {
					budget -= len(groups[i].Expenses)
					continue
				}
				groups[i].Expenses = groups[i].Expenses[:budget]
				budget = 0
			}
		}
	}

	return ReportBuilderResponse{
		Groups:     groups,
		Dimensions: dimensions,
		TotalCount: len(rows),
		TotalUSD:   total,
		From:       q.Range.From,
		To:         q.Range.To,
		Truncated:  truncated,
	}, nil
}

func ParseDimensions(raw string) []string {
	result := []string{}
	for _, part := range strings.Split(raw, ",") {
		normalized := strings.ToLower(strings.TrimSpace(part))
		switch normalized {
		case DimensionTrip, DimensionCity, DimensionCategory:
		default:
			continue
		}
		duplicate := false
		for _, d := range result {
			if d == normalized {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, normalized)
		}
	}
	return result
}

type currencySums struct {
	order []string
	sums  map[string]Decimal
}

func (c *currencySums) add(currency string, amount Decimal) {
	if c.sums == nil {
		c.sums = map[string]Decimal{}
	}
	sum, ok := c.sums[currency]
	if !ok {
		c.order = append(c.order, currency)
	}
	c.sums[currency] = DecimalFrom(sum.Add(amount.Decimal))
}

func (c *currencySums) list() []CurrencyAmount {
	result := make([]CurrencyAmount, 0, len(c.order))
	for _, currency := range c.order {
		result = append(result, CurrencyAmount{Currency: currency, Amount: c.sums[currency]})
	}
	return result
}

func addAmount(amounts []CurrencyAmount, currency string, amount Decimal) []CurrencyAmount {
	for i := range amounts {
		if amounts[i].Currency == currency {
			amounts[i].Amount = DecimalFrom(amounts[i].Amount.Add(amount.Decimal))
			return amounts
		}
	}
	return append(amounts, CurrencyAmount{Currency: currency, Amount: amount})
}

func cultureLess(a, b string) bool {
	ka, kb := cultureKey(a), cultureKey(b)
	if ka != kb {
		return ka < kb
	}
	return a < b
}

func cultureKey(s string) string {
	return textutil.FoldYo(strings.ToLower(s))
}
