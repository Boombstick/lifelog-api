package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"lifelog-api/internal/film"
)

const defaultTopCast = 10

var ErrAdult = errors.New("взрослый контент в каталог не добавляется")

type genreCache struct {
	mu    sync.Mutex
	slugs map[film.Kind]map[int]string
}

func (i *Importer) ImportOne(ctx context.Context, kind film.Kind, tmdbID int) error {
	if kind != film.KindMovie && kind != film.KindSeries {
		return fmt.Errorf("неизвестный вид карточек %q", kind)
	}
	slugs, err := i.cachedGenreSlugs(ctx, kind)
	if err != nil {
		return fmt.Errorf("синхронизация жанров: %w", err)
	}
	prepared, err := i.fetch(ctx, Options{Kind: kind, TopCast: defaultTopCast}, tmdbID, slugs)
	if err != nil {
		return err
	}
	if prepared.Adult {
		return ErrAdult
	}
	return i.storage.SaveFilms(ctx, []PreparedFilm{prepared})
}

func (i *Importer) cachedGenreSlugs(ctx context.Context, kind film.Kind) (map[int]string, error) {
	i.genres.mu.Lock()
	defer i.genres.mu.Unlock()

	if slugs, ok := i.genres.slugs[kind]; ok {
		return slugs, nil
	}
	slugs, err := i.syncGenres(ctx, kind, false)
	if err != nil {
		return nil, err
	}
	if i.genres.slugs == nil {
		i.genres.slugs = make(map[film.Kind]map[int]string)
	}
	i.genres.slugs[kind] = slugs
	return slugs, nil
}

type CardLocalizationStorage interface {
	UntranslatedFilm(ctx context.Context, filmID uint) (*UntranslatedFilm, error)
	IncompletePeopleOfFilm(ctx context.Context, filmID uint) ([]IncompletePerson, error)
	SetFilmTitleRu(ctx context.Context, id uint, title string) error
	UpdatePerson(ctx context.Context, id uint, name, photoURL string) error
}

type CardLocalizer struct {
	source  LocalizationSource
	storage CardLocalizationStorage
	logger  *slog.Logger
}

func NewCardLocalizer(source LocalizationSource, storage CardLocalizationStorage, logger *slog.Logger) *CardLocalizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &CardLocalizer{source: source, storage: storage, logger: logger}
}

func (l *CardLocalizer) Localize(ctx context.Context, filmID uint) error {
	item, err := l.storage.UntranslatedFilm(ctx, filmID)
	if err != nil {
		return err
	}
	if item != nil {
		titles, err := alternativeTitles(ctx, l.source, *item)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			l.logger.Warn("альтернативные названия не получены", "tmdb_id", item.TMDBID, "error", err)
		default:
			if title := russianTitle(titles); title != "" {
				if err := l.storage.SetFilmTitleRu(ctx, item.ID, title); err != nil {
					return err
				}
			}
		}
	}

	people, err := l.storage.IncompletePeopleOfFilm(ctx, filmID)
	if err != nil {
		return err
	}
	for _, item := range people {
		person, err := l.source.Person(ctx, item.TMDBID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			l.logger.Warn("человек не получен", "tmdb_id", item.TMDBID, "error", err)
			continue
		}
		name := russianName(person.AlsoKnownAs)
		photo := l.source.PosterURL(person.ProfilePath)
		if name == "" && photo == "" {
			continue
		}
		if err := l.storage.UpdatePerson(ctx, item.ID, name, photo); err != nil {
			return err
		}
	}
	return nil
}
