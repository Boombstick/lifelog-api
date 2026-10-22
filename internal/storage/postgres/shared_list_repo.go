package postgres

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"
	"lifelog-api/internal/sharedlist"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SharedListRepository struct {
	db *gorm.DB
}

func NewSharedListRepository(db *gorm.DB) *SharedListRepository {
	return &SharedListRepository{db: db}
}

const participantFilter = `(shared_lists.owner_auth_id = ? OR EXISTS (
	SELECT 1 FROM shared_list_members m
	WHERE m.list_id = shared_lists.id AND m.member_auth_id = ?))`

func listNotFound() error { return model.NewEntityNotFound("shared list") }

func (r *SharedListRepository) Access(ctx context.Context, listID uint, caller uuid.UUID) (sharedlist.List, error) {
	var list sharedlist.List
	err := r.db.WithContext(ctx).
		Where("shared_lists.id = ?", listID).
		Where(participantFilter, caller, caller).
		First(&list).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return list, listNotFound()
	}
	return list, err
}

func (r *SharedListRepository) ListsOf(ctx context.Context, caller uuid.UUID) ([]sharedlist.List, error) {
	var lists []sharedlist.List
	err := r.db.WithContext(ctx).
		Where(participantFilter, caller, caller).
		Order("updated_at DESC, id DESC").
		Find(&lists).Error
	return lists, err
}

func (r *SharedListRepository) Create(ctx context.Context, list *sharedlist.List, members []uuid.UUID) error {
	now := dbNow()
	list.CreatedAt, list.UpdatedAt = now, now
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(list).Error; err != nil {
			return err
		}
		return insertMembers(tx, list.ID, members)
	})
}

func insertMembers(tx *gorm.DB, listID uint, members []uuid.UUID) error {
	if len(members) == 0 {
		return nil
	}
	now := dbNow()
	rows := make([]sharedlist.Member, 0, len(members))
	for _, member := range members {
		rows = append(rows, sharedlist.Member{ListID: listID, MemberAuthID: member, CreatedAt: now})
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *SharedListRepository) Update(ctx context.Context, listID uint, title *string, members []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		changes := map[string]interface{}{"updated_at": dbNow()}
		if title != nil {
			changes["title"] = *title
		}
		if err := tx.Model(&sharedlist.List{}).Where("id = ?", listID).Updates(changes).Error; err != nil {
			return err
		}
		if members == nil {
			return nil
		}
		remove := tx.Where("list_id = ?", listID)
		if len(members) > 0 {
			remove = remove.Where("member_auth_id NOT IN ?", members)
		}
		if err := remove.Delete(&sharedlist.Member{}).Error; err != nil {
			return err
		}
		return insertMembers(tx, listID, members)
	})
}

func (r *SharedListRepository) Delete(ctx context.Context, listID uint) error {
	return r.db.WithContext(ctx).Delete(&sharedlist.List{}, listID).Error
}

func (r *SharedListRepository) Leave(ctx context.Context, listID uint, member uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("list_id = ? AND member_auth_id = ?", listID, member).
			Delete(&sharedlist.Member{}).Error; err != nil {
			return err
		}
		return touch(tx, listID)
	})
}

func (r *SharedListRepository) RemoveBlocked(ctx context.Context, a, b uuid.UUID) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM shared_list_members m USING shared_lists l
		WHERE l.id = m.list_id
		  AND ((l.owner_auth_id = ? AND m.member_auth_id = ?) OR (l.owner_auth_id = ? AND m.member_auth_id = ?))`,
		a, b, b, a)
	return result.RowsAffected, result.Error
}

func (r *SharedListRepository) RemoveMembers(ctx context.Context, owner uuid.UUID, members []uuid.UUID) error {
	if len(members) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`DELETE FROM shared_list_members m USING shared_lists l
		WHERE l.id = m.list_id AND l.owner_auth_id = ? AND m.member_auth_id IN ?`,
		owner, members).Error
}

func touch(tx *gorm.DB, listID uint) error {
	return tx.Model(&sharedlist.List{}).Where("id = ?", listID).Update("updated_at", dbNow()).Error
}

func lockList(tx *gorm.DB, listID uint) error {
	var list sharedlist.List
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", listID).First(&list).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return listNotFound()
	}
	return err
}

func (r *SharedListRepository) AddItem(ctx context.Context, listID, filmID uint, by uuid.UUID) (sharedlist.Item, bool, error) {
	var item sharedlist.Item
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockList(tx, listID); err != nil {
			return err
		}

		var films int64
		if err := tx.Model(&film.Film{}).Where("id = ?", filmID).Count(&films).Error; err != nil {
			return err
		}
		if films == 0 {
			return model.NewEntityNotFound("film")
		}

		err := tx.Where("list_id = ? AND film_id = ?", listID, filmID).First(&item).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var count int64
		if err := tx.Model(&sharedlist.Item{}).Where("list_id = ?", listID).Count(&count).Error; err != nil {
			return err
		}
		if count >= sharedlist.MaxItems {
			return sharedlist.ErrListFull
		}

		item = sharedlist.Item{ListID: listID, FilmID: filmID, AddedByAuthID: by, AddedAt: dbNow()}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		created = true
		return touch(tx, listID)
	})
	return item, created, err
}

func (r *SharedListRepository) RemoveItem(ctx context.Context, listID, filmID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("list_id = ? AND film_id = ?", listID, filmID).Delete(&sharedlist.Item{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return model.NewEntityNotFound("shared list item")
		}
		return touch(tx, listID)
	})
}

func (r *SharedListRepository) CreateDraw(
	ctx context.Context,
	listID uint,
	by uuid.UUID,
	mode sharedlist.Mode,
	pick func(filmIDs []uint) []uint,
) (sharedlist.Draw, error) {
	draw := sharedlist.Draw{ListID: listID, Mode: mode, DrawnByAuthID: by}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockList(tx, listID); err != nil {
			return err
		}

		var filmIDs []uint
		if err := tx.Model(&sharedlist.Item{}).Where("list_id = ?", listID).
			Order("film_id").Pluck("film_id", &filmIDs).Error; err != nil {
			return err
		}
		if len(filmIDs) == 0 {
			return sharedlist.ErrListEmpty
		}

		draw.FilmIDs = pick(filmIDs)
		draw.Count = len(draw.FilmIDs)
		draw.CreatedAt = dbNow()
		if err := tx.Create(&draw).Error; err != nil {
			return err
		}
		rows := make([]sharedlist.DrawFilm, 0, len(draw.FilmIDs))
		for position, filmID := range draw.FilmIDs {
			rows = append(rows, sharedlist.DrawFilm{DrawID: draw.ID, Position: position, FilmID: filmID})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}

		if err := tx.Exec(`DELETE FROM shared_list_draws
			WHERE list_id = ? AND id NOT IN (
				SELECT id FROM shared_list_draws WHERE list_id = ?
				ORDER BY created_at DESC, id DESC LIMIT ?)`,
			listID, listID, sharedlist.KeptDraws).Error; err != nil {
			return err
		}
		return touch(tx, listID)
	})
	return draw, err
}

func (r *SharedListRepository) Members(ctx context.Context, listIDs []uint) (map[uint][]uuid.UUID, error) {
	result := map[uint][]uuid.UUID{}
	if len(listIDs) == 0 {
		return result, nil
	}
	var rows []sharedlist.Member
	err := r.db.WithContext(ctx).Raw(`SELECT m.list_id, m.member_auth_id, m.created_at
		FROM shared_list_members m
		WHERE m.list_id IN ?
		ORDER BY m.list_id, m.created_at, m.member_auth_id`, listIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ListID] = append(result[row.ListID], row.MemberAuthID)
	}
	return result, nil
}

func (r *SharedListRepository) ItemCounts(ctx context.Context, listIDs []uint) (map[uint]int, error) {
	result := map[uint]int{}
	if len(listIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		ListID uint
		Count  int
	}
	err := r.db.WithContext(ctx).Model(&sharedlist.Item{}).
		Select("list_id, count(*) AS count").
		Where("list_id IN ?", listIDs).
		Group("list_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ListID] = row.Count
	}
	return result, nil
}

func (r *SharedListRepository) Items(ctx context.Context, listID uint) ([]sharedlist.Item, error) {
	var items []sharedlist.Item
	err := r.db.WithContext(ctx).Where("list_id = ?", listID).
		Order("added_at DESC, film_id DESC").Find(&items).Error
	return items, err
}

func (r *SharedListRepository) Draws(ctx context.Context, listID uint, limit int) ([]sharedlist.Draw, error) {
	var draws []sharedlist.Draw
	err := r.db.WithContext(ctx).Where("list_id = ?", listID).
		Order("created_at DESC, id DESC").Limit(limit).Find(&draws).Error
	if err != nil {
		return nil, err
	}
	return draws, r.fillDrawFilms(ctx, draws)
}

func (r *SharedListRepository) LastDraws(ctx context.Context, listIDs []uint) (map[uint]sharedlist.Draw, error) {
	result := map[uint]sharedlist.Draw{}
	if len(listIDs) == 0 {
		return result, nil
	}
	var draws []sharedlist.Draw
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (list_id) *
		FROM shared_list_draws WHERE list_id IN ?
		ORDER BY list_id, created_at DESC, id DESC`, listIDs).Scan(&draws).Error
	if err != nil {
		return nil, err
	}
	if err := r.fillDrawFilms(ctx, draws); err != nil {
		return nil, err
	}
	for _, draw := range draws {
		result[draw.ListID] = draw
	}
	return result, nil
}

func (r *SharedListRepository) fillDrawFilms(ctx context.Context, draws []sharedlist.Draw) error {
	if len(draws) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(draws))
	for _, draw := range draws {
		ids = append(ids, draw.ID)
	}
	var rows []sharedlist.DrawFilm
	if err := r.db.WithContext(ctx).Where("draw_id IN ?", ids).
		Order("draw_id, position").Find(&rows).Error; err != nil {
		return err
	}
	byDraw := map[uint][]uint{}
	for _, row := range rows {
		byDraw[row.DrawID] = append(byDraw[row.DrawID], row.FilmID)
	}
	for i := range draws {
		draws[i].FilmIDs = byDraw[draws[i].ID]
	}
	return nil
}

func (r *SharedListRepository) Films(ctx context.Context, ids []uint) ([]film.Film, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var films []film.Film
	err := r.db.WithContext(ctx).Preload("Genres").Where("id IN ?", ids).Find(&films).Error
	return films, err
}

func (r *SharedListRepository) Names(ctx context.Context, authIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	result := map[uuid.UUID]string{}
	if len(authIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		AuthUserID uuid.UUID
		Username   string
	}
	err := r.db.WithContext(ctx).Table("users").
		Select("auth_user_id, username").
		Where("auth_user_id IN ? AND deleted_at IS NULL", authIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.AuthUserID] = row.Username
	}
	return result, nil
}

func dbNow() time.Time {
	return time.Now().Truncate(time.Microsecond)
}
