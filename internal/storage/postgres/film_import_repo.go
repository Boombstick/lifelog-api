package postgres

import (
	"context"

	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *FilmRepository) UpsertGenres(ctx context.Context, genres []film.Genre) error {
	if len(genres) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tmdb_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name_ru", "name_en"}),
		}).
		Create(&genres).Error
}

func (r *FilmRepository) GenreSlugsByTMDBID(ctx context.Context) (map[int]string, error) {
	var rows []film.Genre
	if err := r.db.WithContext(ctx).Model(&film.Genre{}).
		Select("id", "tmdb_id").Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make(map[int]string, len(rows))
	for _, row := range rows {
		out[row.TMDBID] = row.ID
	}
	return out, nil
}

func (r *FilmRepository) ExistingFilmTMDBIDs(ctx context.Context, kind film.Kind) (map[int]struct{}, error) {
	var ids []int
	if err := r.db.WithContext(ctx).Model(&film.Film{}).
		Where("kind = ?", kind).
		Pluck("tmdb_id", &ids).Error; err != nil {
		return nil, err
	}

	out := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}

func (r *FilmRepository) SaveFilms(ctx context.Context, films []importer.PreparedFilm) error {
	if len(films) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertCollections(tx, films); err != nil {
			return err
		}

		personIDs, err := upsertPeople(tx, films)
		if err != nil {
			return err
		}

		filmIDs, err := upsertFilms(tx, films)
		if err != nil {
			return err
		}

		if err := linkGenres(tx, films, filmIDs); err != nil {
			return err
		}

		if err := linkCredits(tx, films, filmIDs, personIDs); err != nil {
			return err
		}

		return upsertSeries(tx, films, filmIDs)
	})
}

func upsertPeople(tx *gorm.DB, films []importer.PreparedFilm) (map[int]uint, error) {
	people := make([]film.Person, 0)
	seen := make(map[int]struct{})

	for _, prepared := range films {
		for _, credit := range prepared.Credits {
			if _, dup := seen[credit.Person.TMDBID]; dup {
				continue
			}
			seen[credit.Person.TMDBID] = struct{}{}
			people = append(people, credit.Person)
		}
	}
	if len(people) == 0 {
		return map[int]uint{}, nil
	}

	if err := tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tmdb_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "original_name", "synced_at",
			}),
		}).
		CreateInBatches(&people, 500).Error; err != nil {
		return nil, err
	}

	tmdbIDs := make([]int, 0, len(seen))
	for id := range seen {
		tmdbIDs = append(tmdbIDs, id)
	}

	var stored []film.Person
	if err := tx.Model(&film.Person{}).
		Select("id", "tmdb_id").
		Where("tmdb_id IN ?", tmdbIDs).
		Find(&stored).Error; err != nil {
		return nil, err
	}

	out := make(map[int]uint, len(stored))
	for _, person := range stored {
		out[person.TMDBID] = person.ID
	}
	return out, nil
}

type filmKey struct {
	kind   film.Kind
	tmdbID int
}

func keyOf(f film.Film) filmKey {
	return filmKey{kind: f.Kind, tmdbID: f.TMDBID}
}

func upsertFilms(tx *gorm.DB, prepared []importer.PreparedFilm) (map[filmKey]uint, error) {
	rows := make([]film.Film, 0, len(prepared))
	tmdbIDsByKind := make(map[film.Kind][]int)
	for _, item := range prepared {
		rows = append(rows, item.Film)
		tmdbIDsByKind[item.Film.Kind] = append(tmdbIDsByKind[item.Film.Kind], item.Film.TMDBID)
	}

	if err := tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "kind"}, {Name: "tmdb_id"}},

			DoUpdates: clause.AssignmentColumns([]string{
				"title_en", "title_ru", "synopsis", "poster_url",
				"imdb_id", "tmdb_popularity", "tmdb_poster_path",
				"release_date", "synced_at", "updated_at",
				"runtime", "tmdb_vote_average", "tmdb_vote_count",
				"collection_id",
			}),
		}).
		CreateInBatches(&rows, 200).Error; err != nil {
		return nil, err
	}

	out := make(map[filmKey]uint, len(rows))
	for kind, tmdbIDs := range tmdbIDsByKind {
		var stored []film.Film
		if err := tx.Model(&film.Film{}).
			Select("id", "kind", "tmdb_id").
			Where("kind = ? AND tmdb_id IN ?", kind, tmdbIDs).
			Find(&stored).Error; err != nil {
			return nil, err
		}
		for _, row := range stored {
			out[keyOf(row)] = row.ID
		}
	}
	return out, nil
}

func linkGenres(tx *gorm.DB, films []importer.PreparedFilm, filmIDs map[filmKey]uint) error {
	rows := make([]map[string]any, 0)
	for _, prepared := range films {
		filmID, ok := filmIDs[keyOf(prepared.Film)]
		if !ok {
			continue
		}
		for _, slug := range prepared.GenreSlugs {
			rows = append(rows, map[string]any{
				"film_id":  filmID,
				"genre_id": slug,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	return tx.Table("film_genres").
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, 500).Error
}

func linkCredits(
	tx *gorm.DB,
	films []importer.PreparedFilm,
	filmIDs map[filmKey]uint,
	personIDs map[int]uint,
) error {
	rows := make([]film.FilmCredit, 0)
	for _, prepared := range films {
		filmID, ok := filmIDs[keyOf(prepared.Film)]
		if !ok {
			continue
		}
		for _, credit := range prepared.Credits {
			personID, ok := personIDs[credit.Person.TMDBID]
			if !ok {
				continue
			}
			rows = append(rows, film.FilmCredit{
				FilmID:       filmID,
				PersonID:     personID,
				Role:         credit.Role,
				BillingOrder: credit.BillingOrder,
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	return tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "film_id"}, {Name: "person_id"}, {Name: "role"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"billing_order"}),
		}).
		CreateInBatches(&rows, 500).Error
}

func upsertSeries(tx *gorm.DB, films []importer.PreparedFilm, filmIDs map[filmKey]uint) error {
	var (
		details   []film.SeriesDetails
		seasons   []film.Season
		seriesIDs []uint
	)
	for _, prepared := range films {
		if prepared.Series == nil {
			continue
		}
		filmID, ok := filmIDs[keyOf(prepared.Film)]
		if !ok {
			continue
		}
		row := prepared.Series.Details
		row.FilmID = filmID
		details = append(details, row)
		seriesIDs = append(seriesIDs, filmID)
		for _, season := range prepared.Series.Seasons {
			season.FilmID = filmID
			seasons = append(seasons, season)
		}
	}
	if len(details) == 0 {
		return nil
	}

	if err := tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "film_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "type", "first_air_date", "last_air_date", "next_air_date",
				"number_of_seasons", "number_of_episodes", "in_production",
				"networks", "synced_at",
			}),
		}).
		CreateInBatches(&details, 200).Error; err != nil {
		return err
	}

	if len(seasons) == 0 {
		return nil
	}

	var existing []storedSeason
	if err := tx.Model(&film.Season{}).
		Select("id", "film_id", "tmdb_id", "number").
		Where("film_id IN ?", seriesIDs).
		Find(&existing).Error; err != nil {
		return err
	}

	park, rows := planSeasons(existing, seasons)
	if len(park) > 0 {
		if err := tx.Exec("UPDATE seasons SET number = -id WHERE id IN ?", park).Error; err != nil {
			return err
		}
	}

	return tx.Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tmdb_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"number", "name", "overview", "air_date", "episode_count",
				"poster_url", "synced_at",
			}),
		}).
		CreateInBatches(&rows, 500).Error
}

type storedSeason struct {
	ID     uint
	FilmID uint
	TMDBID int
	Number int
}

type seasonSlot struct {
	filmID uint
	number int
}

func planSeasons(existing []storedSeason, incoming []film.Season) (park []uint, rows []film.Season) {
	target := make(map[int]seasonSlot, len(incoming))
	claimed := make(map[seasonSlot]int, len(incoming))
	for _, season := range incoming {
		slot := seasonSlot{filmID: season.FilmID, number: season.Number}
		if _, dup := target[season.TMDBID]; dup {
			continue
		}
		if _, dup := claimed[slot]; dup {
			continue
		}
		target[season.TMDBID] = slot
		claimed[slot] = season.TMDBID
		rows = append(rows, season)
	}

	for _, stored := range existing {
		if stored.Number == -int(stored.ID) {
			continue
		}
		slot := seasonSlot{filmID: stored.FilmID, number: stored.Number}
		if want, ok := target[stored.TMDBID]; ok {
			if want != slot {
				park = append(park, stored.ID)
			}
			continue
		}
		if _, taken := claimed[slot]; taken {
			park = append(park, stored.ID)
		}
	}
	return park, rows
}

func upsertCollections(tx *gorm.DB, films []importer.PreparedFilm) error {
	collections := make([]film.Collection, 0)
	seen := make(map[int]struct{})

	for _, prepared := range films {
		c := prepared.Collection
		if c == nil {
			continue
		}
		if _, dup := seen[c.TMDBID]; dup {
			continue
		}
		seen[c.TMDBID] = struct{}{}
		collections = append(collections, *c)
	}
	if len(collections) == 0 {
		return nil
	}

	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tmdb_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "poster_url", "poster_path"}),
	}).CreateInBatches(&collections, 200).Error
}
