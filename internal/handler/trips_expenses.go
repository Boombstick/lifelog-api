package handler

import (
	"net/http"
	"strconv"
	"strings"

	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
)

func (h *TripsHandler) addExpense(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.AddExpenseRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	errs.required("description", req.Description)
	if errs.writeIfAny(c) {
		return
	}
	cmd := trips.AddExpenseCommand{
		Amount:      req.Amount,
		Currency:    req.Currency,
		Description: req.Description,
	}
	if req.Date != nil {
		date := req.Date.Time
		cmd.Date = &date
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Expenses.Add(ctx, actor, cmd)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) expenses(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	q := newQuery(c)
	listQuery := trips.ListExpensesQuery{TripID: q.guid("trip_id"), CategoryID: q.long("category_id")}
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Expenses.List(c.Request.Context(), actor, listQuery)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func dateRange(q *query) trips.DateRange {
	return trips.DateRange{From: q.day("from"), To: q.day("to")}
}

func (h *TripsHandler) total(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	q := newQuery(c)
	r := dateRange(q)
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Expenses.Total(c.Request.Context(), actor, r)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) report(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	q := newQuery(c)
	r := dateRange(q)
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Reports.Report(c.Request.Context(), actor, r)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) reportBuilder(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	q := newQuery(c)
	builder := trips.ReportBuilderQuery{
		Range:           dateRange(q),
		Dimensions:      q.text("dimensions"),
		IncludeExpenses: q.flag("include_expenses"),
	}
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Reports.Builder(c.Request.Context(), actor, builder)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) duplicates(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	result, err := h.services.Expenses.Duplicates(c.Request.Context(), actor)
	if err != nil {
		h.fail(c, err)
		return
	}
	if result == nil {
		result = []trips.DuplicatePair{}
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) notDuplicate(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.services.Expenses.NotDuplicate(ctx, actor, id); err != nil {
		h.fail(c, err)
		return
	}
	noContent(c)
}

func (h *TripsHandler) deleteExpense(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.services.Expenses.Delete(ctx, actor, id); err != nil {
		h.fail(c, err)
		return
	}
	noContent(c)
}

func (h *TripsHandler) categories(c *gin.Context) {
	q := newQuery(c)
	includeKeywords := q.flag("include_keywords")
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Dictionary.Categories(c.Request.Context(), includeKeywords)
	if err != nil {
		h.fail(c, err)
		return
	}
	if result == nil {
		result = []trips.CategoryDTO{}
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) createCategory(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.CreateCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	errs.required("name", req.Name)
	errs.requiredList("keywords", req.Keywords)
	if errs.writeIfAny(c) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	id, err := h.services.Dictionary.CreateCategory(ctx, actor, req)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, id)
}

func (h *TripsHandler) addKeywords(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	raw := c.Param("id")
	id, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)

	var req trips.AddKeywordsRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	if parseErr != nil {
		errs.add("id", "Значение '"+raw+"' недопустимо")
	}
	errs.requiredList("keywords", req.Keywords)
	if errs.writeIfAny(c) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.services.Dictionary.AddKeywords(ctx, actor, id, req.Keywords); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, trips.EmptyObject{})
}
