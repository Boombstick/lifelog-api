package service

import (
	"context"
	"time"

	"lifelog-api/internal/search"
)

const searchVisibleAfter = 1500 * time.Millisecond

func (s *SearchService) Invalidate() {
	if s.catalog == nil {
		return
	}
	s.catalog.Clear()
	s.films.Clear()
	s.people.Clear()
}

func (s *SearchService) invalidateNowAndAfterRefresh() {
	s.Invalidate()
	time.AfterFunc(searchVisibleAfter, s.Invalidate)
}

type cardIndexer interface {
	IndexCards(ctx context.Context, filmIDs []uint) error
}

type indexRefresher interface {
	Refresh(ctx context.Context) error
}

type flushingRatings struct {
	inner  LocalRatingIndex
	search *SearchService
}

func FlushingRatingIndex(inner LocalRatingIndex, s *SearchService) LocalRatingIndex {
	return flushingRatings{inner: inner, search: s}
}

func (f flushingRatings) UpdateFilmRating(ctx context.Context, filmID uint, rating float32, votes int) error {
	err := f.inner.UpdateFilmRating(ctx, filmID, rating, votes)
	f.search.invalidateNowAndAfterRefresh()
	return err
}

func (f flushingRatings) UpdateFilmRatings(ctx context.Context, ratings []search.LocalRating) error {
	err := f.inner.UpdateFilmRatings(ctx, ratings)
	f.search.invalidateNowAndAfterRefresh()
	return err
}

type flushingCards struct {
	inner  cardIndexer
	search *SearchService
}

func FlushingCardIndexer(inner cardIndexer, s *SearchService) interface {
	IndexCards(ctx context.Context, filmIDs []uint) error
} {
	return flushingCards{inner: inner, search: s}
}

func (f flushingCards) IndexCards(ctx context.Context, filmIDs []uint) error {
	err := f.inner.IndexCards(ctx, filmIDs)
	f.search.invalidateNowAndAfterRefresh()
	return err
}

type flushingRefresher struct {
	inner  indexRefresher
	search *SearchService
}

func FlushingRefresher(inner indexRefresher, s *SearchService) interface {
	Refresh(ctx context.Context) error
} {
	return flushingRefresher{inner: inner, search: s}
}

func (f flushingRefresher) Refresh(ctx context.Context) error {
	err := f.inner.Refresh(ctx)
	f.search.Invalidate()
	return err
}
