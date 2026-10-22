package indexer

import (
	"context"
	"fmt"
	"log/slog"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

type CardCatalog interface {
	FilmsByIDs(ctx context.Context, ids []uint) ([]film.Film, error)
	PeopleOfFilms(ctx context.Context, filmIDs []uint) ([]PersonRow, error)
}

type CardEngine interface {
	IndexFilms(ctx context.Context, docs []search.FilmDoc) ([]search.BulkFailure, error)
	IndexPeople(ctx context.Context, docs []search.PersonDoc) ([]search.BulkFailure, error)
}

type CardIndexer struct {
	catalog CardCatalog
	engine  CardEngine
	logger  *slog.Logger
}

func NewCardIndexer(catalog CardCatalog, engine CardEngine, logger *slog.Logger) *CardIndexer {
	if logger == nil {
		logger = slog.Default()
	}
	return &CardIndexer{catalog: catalog, engine: engine, logger: logger}
}

func (i *CardIndexer) IndexCards(ctx context.Context, filmIDs []uint) error {
	if len(filmIDs) == 0 {
		return nil
	}
	films, err := i.catalog.FilmsByIDs(ctx, filmIDs)
	if err != nil {
		return err
	}
	docs := make([]search.FilmDoc, 0, len(films))
	for _, item := range films {
		docs = append(docs, filmToDoc(item))
	}
	refused, err := i.engine.IndexFilms(ctx, docs)
	if err != nil {
		return err
	}
	if err := refusedError(refused); err != nil {
		return err
	}

	people, err := i.catalog.PeopleOfFilms(ctx, filmIDs)
	if err != nil {
		return err
	}
	personDocs := make([]search.PersonDoc, 0, len(people))
	for _, row := range people {
		personDocs = append(personDocs, personToDoc(row))
	}
	refused, err = i.engine.IndexPeople(ctx, personDocs)
	if err != nil {
		return err
	}
	return refusedError(refused)
}

func refusedError(refused []search.BulkFailure) error {
	if len(refused) == 0 {
		return nil
	}
	first := refused[0]
	return fmt.Errorf("%w: %d, first %s: %s: %s",
		ErrDocumentsFailed, len(refused), first.ID, first.Type, truncate(first.Reason, maxReasonLen))
}
