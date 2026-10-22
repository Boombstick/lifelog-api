package handler

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"

	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *TripsHandler) photos(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	q := newQuery(c)
	tripID := q.guid("trip_id")
	if q.writeIfInvalid(c) {
		return
	}
	result, err := h.services.Photos.List(c.Request.Context(), actor, tripID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) addPhoto(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	if c.Request.ContentLength > trips.MaxPhotoRequestBytes {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trips.MaxPhotoRequestBytes)

	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		writeValidationProblem(c, problems{"file": {"Поле file обязательно"}})
		return
	}
	if err := c.Request.ParseMultipartForm(trips.MaxPhotoRequestBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		writeValidationProblem(c, problems{"": {"Не удалось прочитать форму запроса: " + err.Error()}})
		return
	}
	form := c.Request.MultipartForm
	defer func() { _ = form.RemoveAll() }()

	errs := problems{}
	file := formFile(form, "file")
	if file == nil {
		errs.add("file", "Поле file обязательно")
	}
	var tripID *uuid.UUID
	if raw, ok := formValue(form, "trip_id"); ok && strings.TrimSpace(raw) != "" {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			errs.add("trip_id", "Значение '"+raw+"' не подходит для trip_id")
		} else {
			tripID = &id
		}
	}
	if errs.writeIfAny(c) {
		return
	}

	var caption *string
	if raw, ok := formValue(form, "caption"); ok {
		caption = &raw
	}
	body, err := file.Open()
	if err != nil {
		h.fail(c, err)
		return
	}
	defer body.Close()

	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Photos.Add(ctx, actor, trips.AddPhotoCommand{
		TripID:      tripID,
		Caption:     caption,
		FileName:    file.Filename,
		ContentType: file.Header.Get("Content-Type"),
		Size:        file.Size,
		Body:        body,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func formFile(form *multipart.Form, name string) *multipart.FileHeader {
	for key, files := range form.File {
		if !strings.EqualFold(key, name) {
			continue
		}
		for _, f := range files {
			if f.Size == 0 && f.Filename == "" {
				continue
			}
			return f
		}
	}
	return nil
}

func formValue(form *multipart.Form, name string) (string, bool) {
	if v, ok := form.Value[name]; ok && len(v) > 0 {
		return v[0], true
	}
	for key, v := range form.Value {
		if strings.EqualFold(key, name) && len(v) > 0 {
			return v[0], true
		}
	}
	return "", false
}

func (h *TripsHandler) downloadPhoto(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	photoID, _ := uuid.Parse(strings.TrimSpace(c.Param("photoId")))
	download, err := h.services.Photos.Open(c.Request.Context(), actor, photoID)
	if err != nil {
		h.fail(c, err)
		return
	}
	defer download.Body.Close()

	c.Header("Content-Type", download.ContentType)
	if download.FileName != "" {
		c.Header("Content-Disposition", contentDisposition(download.FileName))
	}
	if stat, ok := download.Body.(interface{ Stat() (os.FileInfo, error) }); ok {
		if info, err := stat.Stat(); err == nil {
			c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
		}
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, download.Body); err != nil {
		h.logger.WarnContext(c.Request.Context(), "trips: photo download interrupted", "photo_id", photoID, "error", err)
	}
}

func (h *TripsHandler) deletePhoto(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	photoID, _ := uuid.Parse(strings.TrimSpace(c.Param("photoId")))
	ctx, cancel := writeContext(c)
	defer cancel()
	if err := h.services.Photos.Delete(ctx, actor, photoID); err != nil {
		h.fail(c, err)
		return
	}
	noContent(c)
}

func contentDisposition(name string) string {
	var plain strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e {
			plain.WriteByte('_')
		} else {
			plain.WriteRune(r)
		}
	}
	filename := plain.String()
	if !isHTTPToken(filename) {
		filename = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename) + `"`
	}
	return "attachment; filename=" + filename + "; filename*=UTF-8''" + encodeRFC5987(name)
}

func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if b := s[i]; b <= ' ' || b >= 0x7f || strings.IndexByte(`()<>@,;:\"/[]?={}`, b) >= 0 {
			return false
		}
	}
	return true
}

func encodeRFC5987(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if isAttrChar(ch) {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[ch>>4])
		b.WriteByte(hexDigits[ch&0x0f])
	}
	return b.String()
}

func isAttrChar(ch byte) bool {
	switch {
	case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", ch) >= 0
}
