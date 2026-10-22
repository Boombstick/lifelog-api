package handler

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"lifelog-api/internal/model"
	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.html
var sharedPages embed.FS

const sharedPageLimit = 50

type sharedPageItem struct {
	Title       string
	TitleOrig   string
	PosterURL   string
	Meta        string
	Rate        *int16
	Description *string
}

type sharedPageData struct {
	Title    string
	Heading  string
	Subtitle string
	Items    []sharedPageItem
	Total    int
	Expires  string
	Shown    int
	More     bool
}

func (h *SharingHandler) SharedPage(ctx *gin.Context) {
	list, err := h.service.SharedList(ctx, ctx.Param("token"), sharedPageLimit, "")
	if err != nil {
		if _, ok := errors.AsType[*model.EntityNotFoundError](err); ok {
			h.renderShared(ctx, http.StatusNotFound, sharedPageData{
				Title:    "Ссылка не найдена",
				Heading:  "Ссылка не работает",
				Subtitle: "Она истекла, её отозвали или она никогда не существовала.",
			})
			return
		}
		h.logger.Error("страница по ссылке не собрана", "error", err)
		h.renderShared(ctx, http.StatusInternalServerError, sharedPageData{
			Title:    "Ошибка",
			Heading:  "Не получилось открыть список",
			Subtitle: "Попробуйте позже.",
		})
		return
	}

	data := sharedPageData{
		Title:    fmt.Sprintf("%s — %s", setTitle(list.Set), list.Owner.DisplayName),
		Heading:  setTitle(list.Set),
		Subtitle: subtitleOf(list),
		Total:    list.Total,
		Expires:  humanExpiry(list.ExpiresAt),
		Shown:    len(list.Items),
		More:     list.Total > len(list.Items),
	}
	for _, item := range list.Items {
		data.Items = append(data.Items, sharedPageItem{
			Title:       item.Title,
			TitleOrig:   item.TitleOrig,
			PosterURL:   item.PosterURL,
			Meta:        metaOf(item),
			Rate:        item.Rate,
			Description: item.Description,
		})
	}
	h.renderShared(ctx, http.StatusOK, data)
}

func (h *SharingHandler) renderShared(ctx *gin.Context, code int, data sharedPageData) {
	ctx.Header("X-Robots-Tag", "noindex, nofollow")
	ctx.Status(code)
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.pages.ExecuteTemplate(ctx.Writer, "shared.html", data); err != nil {
		h.logger.Error("страница не отрисована", "error", err)
	}
}

func setTitle(set string) string {
	switch set {
	case "watched":
		return "Просмотренное"
	case "rated":
		return "Оценённое"
	case "watchlist":
		return "Хочу посмотреть"
	default:
		return "Фильм"
	}
}

func subtitleOf(list *service.SharedList) string {
	owner := strings.TrimSpace(list.Owner.DisplayName)
	if owner == "" {
		return fmt.Sprintf("%d %s", list.Total, films(list.Total))
	}
	return fmt.Sprintf("%s · %d %s", owner, list.Total, films(list.Total))
}

func metaOf(item service.MyFilmItem) string {
	parts := make([]string, 0, 3)
	if item.Year > 0 {
		parts = append(parts, fmt.Sprint(item.Year))
	}
	if item.Runtime > 0 {
		parts = append(parts, fmt.Sprintf("%d мин", item.Runtime))
	}
	if item.Kind == "series" {
		parts = append(parts, "сериал")
	}
	return strings.Join(parts, " · ")
}

func films(count int) string {
	tail := count % 100
	if tail >= 11 && tail <= 14 {
		return "фильмов"
	}
	switch count % 10 {
	case 1:
		return "фильм"
	case 2, 3, 4:
		return "фильма"
	default:
		return "фильмов"
	}
}

func mustParsePages() *template.Template {
	return template.Must(template.ParseFS(sharedPages, "templates/*.html"))
}

func humanExpiry(expires time.Time) string {
	left := time.Until(expires)
	switch {
	case left <= 0:
		return ""
	case left < time.Hour:
		return "Ссылка перестанет работать меньше чем через час"
	case left < 24*time.Hour:
		hours := int(left.Hours())
		return fmt.Sprintf("Ссылка перестанет работать примерно через %d %s", hours, hoursWord(hours))
	default:
		days := int(left.Hours() / 24)
		return fmt.Sprintf("Ссылка перестанет работать примерно через %d %s", days, daysWord(days))
	}
}

func hoursWord(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "час"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "часа"
	default:
		return "часов"
	}
}

func daysWord(n int) string {
	if n == 1 {
		return "сутки"
	}
	return "суток"
}
