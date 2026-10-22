package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"lifelog-api/internal/note"

	"github.com/gin-gonic/gin"
)

type NotesHandler struct {
	handler
	logger  *slog.Logger
	service *note.Service
}

func NewNotesHandler(logger *slog.Logger, service *note.Service) *NotesHandler {
	return &NotesHandler{logger: logger, service: service}
}

func (h *NotesHandler) Register(engine *gin.Engine, auth gin.HandlerFunc) {
	group := engine.Group("/notes", auth)
	group.GET("", h.list)
	group.POST("", h.create)
	group.GET("/tags", h.tags)
	group.GET("/:id", h.get)
	group.PATCH("/:id", h.update)
	group.DELETE("/:id", h.delete)
}

type optionalField struct {
	Set   bool
	Value json.RawMessage
}

func (f *optionalField) UnmarshalJSON(data []byte) error {
	f.Set = true
	f.Value = append(f.Value[:0], data...)
	return nil
}

func (f optionalField) null() bool {
	return bytes.Equal(bytes.TrimSpace(f.Value), []byte("null"))
}

func (f optionalField) decode(target any) error {
	if !f.Set || f.null() {
		return nil
	}
	return json.Unmarshal(f.Value, target)
}

type noteCreateRequest struct {
	Title *string  `json:"title"`
	Body  string   `json:"body"`
	Day   *string  `json:"day"`
	Tags  []string `json:"tags"`
}

type noteUpdateBody struct {
	Title optionalField `json:"title"`
	Body  optionalField `json:"body"`
	Day   optionalField `json:"day"`
	Tags  optionalField `json:"tags"`
}

var (
	errNoteBodyNull = errors.New("body не может быть null")
	errNoteField    = errors.New("неверный тип поля")
)

func (b noteUpdateBody) input() (note.UpdateInput, error) {
	var input note.UpdateInput
	if b.Title.Set {
		input.SetTitle = true
		if err := b.Title.decode(&input.Title); err != nil {
			return input, errors.Join(errNoteField, errors.New("title: строка или null"))
		}
	}
	if b.Body.Set {
		if b.Body.null() {
			return input, errNoteBodyNull
		}
		var body string
		if err := b.Body.decode(&body); err != nil {
			return input, errors.Join(errNoteField, errors.New("body: строка"))
		}
		input.Body = &body
	}
	if b.Day.Set {
		input.SetDay = true
		if err := b.Day.decode(&input.Day); err != nil {
			return input, note.ErrBadDay
		}
	}
	if b.Tags.Set {
		input.SetTags = true
		if err := b.Tags.decode(&input.Tags); err != nil {
			return input, errors.Join(errNoteField, errors.New("tags: массив строк"))
		}
	}
	return input, nil
}

func (h *NotesHandler) noteID(c *gin.Context) (uint, bool) {
	id, err := h.paramID(c)
	if err != nil {
		h.notFoundJSON(c, note.ErrNotFound)
		return 0, false
	}
	return id, true
}

func (h *NotesHandler) fail(c *gin.Context, logMsg string, err error) {
	switch {
	case errors.Is(err, note.ErrNotFound):
		h.notFoundJSON(c, err)
	case errors.Is(err, note.ErrTitleTooLong),
		errors.Is(err, note.ErrBodyEmpty),
		errors.Is(err, note.ErrBodyTooLong),
		errors.Is(err, note.ErrTagEmpty),
		errors.Is(err, note.ErrTagTooLong),
		errors.Is(err, note.ErrTooManyTags),
		errors.Is(err, note.ErrBadDay),
		errors.Is(err, note.ErrBadCursor),
		errors.Is(err, note.ErrBadLimit),
		errors.Is(err, errNoteBodyNull),
		errors.Is(err, errNoteField):
		h.badRequestJSON(c, err)
	default:
		h.logger.Error(logMsg, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
	}
}

func (h *NotesHandler) list(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	limit, err := note.PageSize(c.Query("limit"))
	if err != nil {
		h.badRequestJSON(c, err)
		return
	}
	page, err := h.service.List(c.Request.Context(), userID, note.ListInput{
		Tags:   c.QueryArray("tag"),
		Cursor: c.Query("cursor"),
		Limit:  limit,
	})
	if err != nil {
		h.fail(c, "notes list failed", err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *NotesHandler) create(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	var request noteCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, bindErrorMessage(err, &request))
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	created, err := h.service.Create(ctx, userID, note.CreateInput{
		Title: request.Title,
		Body:  request.Body,
		Day:   request.Day,
		Tags:  request.Tags,
	})
	if err != nil {
		h.fail(c, "note not created", err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *NotesHandler) get(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, ok := h.noteID(c)
	if !ok {
		return
	}
	found, err := h.service.Get(c.Request.Context(), userID, id)
	if err != nil {
		h.fail(c, "note not read", err)
		return
	}
	c.JSON(http.StatusOK, found)
}

func (h *NotesHandler) update(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, ok := h.noteID(c)
	if !ok {
		return
	}
	var body noteUpdateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, bindErrorMessage(err, &body))
		return
	}
	input, err := body.input()
	if err != nil {
		h.fail(c, "note update rejected", err)
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	updated, err := h.service.Update(ctx, userID, id, input)
	if err != nil {
		h.fail(c, "note not updated", err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *NotesHandler) delete(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	id, ok := h.noteID(c)
	if !ok {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.service.Delete(ctx, userID, id); err != nil {
		h.fail(c, "note not deleted", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *NotesHandler) tags(c *gin.Context) {
	userID, ok := requireAccount(c, h.logger)
	if !ok {
		return
	}
	tags, err := h.service.Tags(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, "note tags not read", err)
		return
	}
	c.JSON(http.StatusOK, tags)
}
