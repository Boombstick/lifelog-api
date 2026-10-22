package imdbimport

import (
	"strings"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

type TitleClass int

const (
	ClassUnknown TitleClass = iota
	ClassMovie
	ClassSeries
	ClassUnsupported
)

func ClassifyTitleType(value string) TitleClass {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	switch key {
	case "":
		return ClassUnknown
	case "movie", "tvmovie", "short", "video":
		return ClassMovie
	case "tvseries", "tvminiseries":
		return ClassSeries
	}
	return ClassUnsupported
}

type PickResult int

const (
	PickFound PickResult = iota
	PickNotFound
	PickUnsupported
)

func PickCard(found tmdb.FindResult, class TitleClass) (film.Kind, int, PickResult) {
	switch {
	case len(found.TVIDs) > 0 && (class == ClassSeries || len(found.MovieIDs) == 0):
		return film.KindSeries, found.TVIDs[0], PickFound
	case len(found.MovieIDs) > 0:
		return film.KindMovie, found.MovieIDs[0], PickFound
	case found.Other:
		return "", 0, PickUnsupported
	}
	return "", 0, PickNotFound
}

type Snapshot struct {
	Watchlisted bool
	Watched     bool
	Rated       bool
	Reviewed    bool
	Dropped     bool

	SeasonEntries int
}

func (s Snapshot) HasAnyMark() bool {
	return s.Watchlisted || s.Watched || s.Rated || s.Reviewed || s.Dropped || s.SeasonEntries > 0
}

type Action int

const (
	ActionSkip Action = iota
	ActionRateMovie
	ActionRateSeasons
	ActionWatchlist
)

func Decide(list ListKind, kind film.Kind, snapshot Snapshot, airedSeasons int) Action {
	if list == ListWatchlist {
		if snapshot.HasAnyMark() {
			return ActionSkip
		}
		return ActionWatchlist
	}

	if kind != film.KindSeries {
		if snapshot.Watched || snapshot.Rated {
			return ActionSkip
		}
		return ActionRateMovie
	}

	if snapshot.SeasonEntries > 0 || snapshot.Dropped {
		return ActionSkip
	}
	if airedSeasons == 0 {
		if snapshot.Watchlisted {
			return ActionSkip
		}
		return ActionWatchlist
	}
	return ActionRateSeasons
}
