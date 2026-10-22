package service

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"
	"lifelog-api/internal/sharedlist"

	"github.com/google/uuid"
)

var (
	ErrSharedListTitle      = fmt.Errorf("название — от 1 до %d символов", sharedlist.MaxTitleLength)
	ErrSharedListSelfMember = errors.New("себя указывать в участниках не нужно")
	ErrSharedListTooMany    = fmt.Errorf("участников не больше %d", sharedlist.MaxMembers)
	ErrSharedListNotOwner   = errors.New("это может только создатель списка")
	ErrSharedListOwnerLeave = errors.New("создатель не может выйти из списка — его можно удалить")
	ErrSharedListBadMode    = errors.New("mode: one или top")
	ErrSharedListBadCount   = fmt.Errorf("count для top — от %d до %d", sharedlist.MinTopCount, sharedlist.MaxTopCount)
	ErrSharedListBadFilm    = errors.New("film_id обязателен")
)

type NotSubscribersError struct {
	IDs []uuid.UUID
}

func (e *NotSubscribersError) Error() string {
	ids := make([]string, 0, len(e.IDs))
	for _, id := range e.IDs {
		ids = append(ids, id.String())
	}
	return "участник должен быть вашим подписчиком: " + strings.Join(ids, ", ")
}

type BadMemberIDError struct {
	Value string
}

func (e *BadMemberIDError) Error() string {
	return "member_ids: не uuid: " + e.Value
}

type SharedListRepository interface {
	Access(ctx context.Context, listID uint, caller uuid.UUID) (sharedlist.List, error)
	ListsOf(ctx context.Context, caller uuid.UUID) ([]sharedlist.List, error)
	Create(ctx context.Context, list *sharedlist.List, members []uuid.UUID) error
	Update(ctx context.Context, listID uint, title *string, members []uuid.UUID) error
	Delete(ctx context.Context, listID uint) error
	Leave(ctx context.Context, listID uint, member uuid.UUID) error
	AddItem(ctx context.Context, listID, filmID uint, by uuid.UUID) (sharedlist.Item, bool, error)
	RemoveItem(ctx context.Context, listID, filmID uint) error
	CreateDraw(ctx context.Context, listID uint, by uuid.UUID, mode sharedlist.Mode, pick func([]uint) []uint) (sharedlist.Draw, error)
	Members(ctx context.Context, listIDs []uint) (map[uint][]uuid.UUID, error)
	ItemCounts(ctx context.Context, listIDs []uint) (map[uint]int, error)
	Items(ctx context.Context, listID uint) ([]sharedlist.Item, error)
	Draws(ctx context.Context, listID uint, limit int) ([]sharedlist.Draw, error)
	LastDraws(ctx context.Context, listIDs []uint) (map[uint]sharedlist.Draw, error)
	Films(ctx context.Context, ids []uint) ([]film.Film, error)
	Names(ctx context.Context, authIDs []uuid.UUID) (map[uuid.UUID]string, error)
	RemoveBlocked(ctx context.Context, a, b uuid.UUID) (int64, error)
	RemoveMembers(ctx context.Context, owner uuid.UUID, members []uuid.UUID) error
}

type BlockCheck interface {
	Blocked(ctx context.Context, user uuid.UUID) (blocked map[uuid.UUID]struct{}, known bool)
}

var ErrBlocksUnavailable = errors.New("не удалось проверить блокировки, повторите позже")

type FollowerCheck interface {
	FollowersAmong(ctx context.Context, target uuid.UUID, candidates []uuid.UUID) (map[uuid.UUID]bool, error)
}

var ErrFollowsUnavailable = errors.New("не удалось проверить подписки, повторите позже")

type UserRef struct {
	AuthUserID uuid.UUID `json:"auth_user_id" swaggertype:"string" format:"uuid"`
	Name       *string   `json:"name"`
}

type SharedListDraw struct {
	ID        uint             `json:"id"`
	Mode      string           `json:"mode"`
	Count     int              `json:"count"`
	Films     []FilmSearchItem `json:"films"`
	DrawnBy   UserRef          `json:"drawn_by"`
	CreatedAt time.Time        `json:"created_at"`
}

type SharedListItem struct {
	Film    FilmSearchItem `json:"film"`
	AddedBy UserRef        `json:"added_by"`
	AddedAt time.Time      `json:"added_at"`
}

type SharedListSummary struct {
	ID           uint            `json:"id"`
	Title        string          `json:"title"`
	Owner        UserRef         `json:"owner"`
	IsOwner      bool            `json:"is_owner"`
	MembersCount int             `json:"members_count"`
	ItemsCount   int             `json:"items_count"`
	LastDraw     *SharedListDraw `json:"last_draw"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type SharedListCard struct {
	SharedListSummary
	Members []UserRef        `json:"members"`
	Items   []SharedListItem `json:"items"`
	Draws   []SharedListDraw `json:"draws"`
}

type SharedListsPage struct {
	Items []SharedListSummary `json:"items"`
}

type SharedListDrawsPage struct {
	Items []SharedListDraw `json:"items"`
}

const (
	cardDraws        = 10
	defaultDrawsPage = 20
)

type SharedListService struct {
	repo      SharedListRepository
	followers FollowerCheck
	blocks    BlockCheck
	logger    *slog.Logger

	newRNG func() *rand.Rand
}

func NewSharedListService(repo SharedListRepository, followers FollowerCheck, blocks BlockCheck, logger *slog.Logger) *SharedListService {
	if logger == nil {
		logger = slog.Default()
	}
	return &SharedListService{repo: repo, followers: followers, blocks: blocks, logger: logger, newRNG: cryptoSeededRNG}
}

func (s *SharedListService) ApplyBlock(ctx context.Context, a, b uuid.UUID) (int64, error) {
	return s.repo.RemoveBlocked(ctx, a, b)
}

type blockState struct {
	blocked map[uuid.UUID]struct{}
	known   bool
}

func (s *SharedListService) ownerBlocks(ctx context.Context, owner uuid.UUID) blockState {
	if s.blocks == nil {
		return blockState{known: true}
	}
	blocked, known := s.blocks.Blocked(ctx, owner)
	return blockState{blocked: blocked, known: known}
}

func (b blockState) visible(members []uuid.UUID) (kept, blocked []uuid.UUID) {
	if !b.known || len(b.blocked) == 0 {
		return members, nil
	}
	kept = make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if _, ok := b.blocked[member]; ok {
			blocked = append(blocked, member)
			continue
		}
		kept = append(kept, member)
	}
	return kept, blocked
}

func (s *SharedListService) dropBlocked(ctx context.Context, owner uuid.UUID, members []uuid.UUID) {
	if len(members) == 0 {
		return
	}
	dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.repo.RemoveMembers(dropCtx, owner, members); err != nil {
		s.logger.Warn("blocked members not removed from shared lists", "owner", owner, "members", members, "error", err)
		return
	}
	s.logger.Info("blocked members removed from shared lists on read", "owner", owner, "members", members)
}

func (s *SharedListService) access(ctx context.Context, listID uint, caller uuid.UUID) (sharedlist.List, error) {
	list, err := s.repo.Access(ctx, listID, caller)
	if err != nil || list.OwnerAuthID == caller {
		return list, err
	}
	state := s.ownerBlocks(ctx, list.OwnerAuthID)
	if !state.known {
		return sharedlist.List{}, ErrBlocksUnavailable
	}
	if _, blocked := state.blocked[caller]; blocked {
		s.dropBlocked(ctx, list.OwnerAuthID, []uuid.UUID{caller})
		return sharedlist.List{}, model.NewEntityNotFound("shared list")
	}
	return list, nil
}

func cryptoSeededRNG() *rand.Rand {
	var seed [32]byte
	_, _ = crand.Read(seed[:])
	return rand.New(rand.NewChaCha8(seed))
}

func normalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(title)
	if length == 0 || length > sharedlist.MaxTitleLength {
		return "", ErrSharedListTitle
	}
	return title, nil
}

func parseMembers(owner uuid.UUID, raw []string) ([]uuid.UUID, error) {
	members := make([]uuid.UUID, 0, len(raw))
	seen := map[uuid.UUID]bool{}
	for _, value := range raw {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, &BadMemberIDError{Value: value}
		}
		if id == owner {
			return nil, ErrSharedListSelfMember
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		members = append(members, id)
	}
	if len(members) > sharedlist.MaxMembers {
		return nil, ErrSharedListTooMany
	}
	return members, nil
}

func (s *SharedListService) validateMembers(ctx context.Context, owner uuid.UUID, raw []string, current []uuid.UUID) ([]uuid.UUID, error) {
	members, err := parseMembers(owner, raw)
	if err != nil {
		return nil, err
	}
	var added []uuid.UUID
	for _, member := range members {
		if !slices.Contains(current, member) {
			added = append(added, member)
		}
	}
	if len(added) == 0 {
		return members, nil
	}
	if s.followers == nil {
		return nil, ErrFollowsUnavailable
	}
	subscribed, err := s.followers.FollowersAmong(ctx, owner, added)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFollowsUnavailable, err)
	}
	var rejected []uuid.UUID
	for _, member := range added {
		if !subscribed[member] {
			rejected = append(rejected, member)
		}
	}
	if len(rejected) > 0 {
		return nil, &NotSubscribersError{IDs: rejected}
	}
	return members, nil
}

func drawSize(mode string, count *int) (int, error) {
	switch sharedlist.Mode(mode) {
	case sharedlist.ModeOne:
		return 1, nil
	case sharedlist.ModeTop:
		if count == nil || *count < sharedlist.MinTopCount || *count > sharedlist.MaxTopCount {
			return 0, ErrSharedListBadCount
		}
		return *count, nil
	default:
		return 0, ErrSharedListBadMode
	}
}

func drawFilms(ids []uint, n int, rng *rand.Rand) []uint {
	pool := slices.Clone(ids)
	n = min(n, len(pool))
	for i := range n {
		j := i + rng.IntN(len(pool)-i)
		pool[i], pool[j] = pool[j], pool[i]
	}
	return pool[:n]
}

func (s *SharedListService) Lists(ctx context.Context, caller uuid.UUID) (SharedListsPage, error) {
	lists, err := s.repo.ListsOf(ctx, caller)
	if err != nil {
		return SharedListsPage{}, err
	}
	states := map[uuid.UUID]blockState{}
	kept := make([]sharedlist.List, 0, len(lists))
	for _, list := range lists {
		state, ok := states[list.OwnerAuthID]
		if !ok {
			state = s.ownerBlocks(ctx, list.OwnerAuthID)
			states[list.OwnerAuthID] = state
		}
		if list.OwnerAuthID != caller {
			if !state.known {
				continue
			}
			if _, blocked := state.blocked[caller]; blocked {
				s.dropBlocked(ctx, list.OwnerAuthID, []uuid.UUID{caller})
				continue
			}
		}
		kept = append(kept, list)
	}
	summaries, err := s.summaries(ctx, kept, caller, states)
	if err != nil {
		return SharedListsPage{}, err
	}
	return SharedListsPage{Items: summaries}, nil
}

func (s *SharedListService) Create(ctx context.Context, caller uuid.UUID, title string, memberIDs []string) (SharedListCard, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return SharedListCard{}, err
	}
	members, err := s.validateMembers(ctx, caller, memberIDs, nil)
	if err != nil {
		return SharedListCard{}, err
	}
	list := sharedlist.List{OwnerAuthID: caller, Title: title}
	if err := s.repo.Create(ctx, &list, members); err != nil {
		return SharedListCard{}, err
	}
	return s.card(ctx, list, caller)
}

func (s *SharedListService) Get(ctx context.Context, listID uint, caller uuid.UUID) (SharedListCard, error) {
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return SharedListCard{}, err
	}
	return s.card(ctx, list, caller)
}

func (s *SharedListService) ownedList(ctx context.Context, listID uint, caller uuid.UUID) (sharedlist.List, error) {
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return list, err
	}
	if list.OwnerAuthID != caller {
		return list, ErrSharedListNotOwner
	}
	return list, nil
}

func (s *SharedListService) Update(ctx context.Context, listID uint, caller uuid.UUID, title *string, memberIDs []string) (SharedListCard, error) {
	list, err := s.ownedList(ctx, listID, caller)
	if err != nil {
		return SharedListCard{}, err
	}
	var newTitle *string
	if title != nil {
		normalized, err := normalizeTitle(*title)
		if err != nil {
			return SharedListCard{}, err
		}
		newTitle = &normalized
	}
	var members []uuid.UUID
	if memberIDs != nil {
		current, err := s.repo.Members(ctx, []uint{list.ID})
		if err != nil {
			return SharedListCard{}, err
		}

		standing, _ := s.ownerBlocks(ctx, caller).visible(current[list.ID])
		members, err = s.validateMembers(ctx, caller, memberIDs, standing)
		if err != nil {
			return SharedListCard{}, err
		}
	}
	if err := s.repo.Update(ctx, list.ID, newTitle, members); err != nil {
		return SharedListCard{}, err
	}
	return s.Get(ctx, list.ID, caller)
}

func (s *SharedListService) Delete(ctx context.Context, listID uint, caller uuid.UUID) error {
	list, err := s.ownedList(ctx, listID, caller)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, list.ID)
}

func (s *SharedListService) Leave(ctx context.Context, listID uint, caller uuid.UUID) error {
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return err
	}
	if list.OwnerAuthID == caller {
		return ErrSharedListOwnerLeave
	}
	return s.repo.Leave(ctx, list.ID, caller)
}

func (s *SharedListService) AddItem(ctx context.Context, listID uint, caller uuid.UUID, filmID uint) (SharedListItem, bool, error) {
	if filmID == 0 {
		return SharedListItem{}, false, ErrSharedListBadFilm
	}
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return SharedListItem{}, false, err
	}
	item, created, err := s.repo.AddItem(ctx, list.ID, filmID, caller)
	if err != nil {
		return SharedListItem{}, false, err
	}
	films, err := s.filmItems(ctx, []uint{item.FilmID})
	if err != nil {
		return SharedListItem{}, false, err
	}
	names, err := s.repo.Names(ctx, []uuid.UUID{item.AddedByAuthID})
	if err != nil {
		return SharedListItem{}, false, err
	}
	return SharedListItem{
		Film:    films[item.FilmID],
		AddedBy: userRef(item.AddedByAuthID, names),
		AddedAt: item.AddedAt.UTC(),
	}, created, nil
}

func (s *SharedListService) RemoveItem(ctx context.Context, listID uint, caller uuid.UUID, filmID uint) error {
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return err
	}
	return s.repo.RemoveItem(ctx, list.ID, filmID)
}

func (s *SharedListService) Draw(ctx context.Context, listID uint, caller uuid.UUID, mode string, count *int) (SharedListDraw, error) {
	size, err := drawSize(mode, count)
	if err != nil {
		return SharedListDraw{}, err
	}
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return SharedListDraw{}, err
	}
	rng := s.newRNG()
	draw, err := s.repo.CreateDraw(ctx, list.ID, caller, sharedlist.Mode(mode), func(ids []uint) []uint {
		return drawFilms(ids, size, rng)
	})
	if err != nil {
		return SharedListDraw{}, err
	}
	views, err := s.drawViews(ctx, []sharedlist.Draw{draw})
	if err != nil {
		return SharedListDraw{}, err
	}
	return views[0], nil
}

func (s *SharedListService) Draws(ctx context.Context, listID uint, caller uuid.UUID, limit int) (SharedListDrawsPage, error) {
	if limit <= 0 {
		limit = defaultDrawsPage
	}
	limit = min(limit, sharedlist.KeptDraws)
	list, err := s.access(ctx, listID, caller)
	if err != nil {
		return SharedListDrawsPage{}, err
	}
	draws, err := s.repo.Draws(ctx, list.ID, limit)
	if err != nil {
		return SharedListDrawsPage{}, err
	}
	views, err := s.drawViews(ctx, draws)
	if err != nil {
		return SharedListDrawsPage{}, err
	}
	return SharedListDrawsPage{Items: views}, nil
}

func (s *SharedListService) summaries(ctx context.Context, lists []sharedlist.List, caller uuid.UUID, states map[uuid.UUID]blockState) ([]SharedListSummary, error) {
	result := make([]SharedListSummary, 0, len(lists))
	if len(lists) == 0 {
		return result, nil
	}
	ids := make([]uint, 0, len(lists))
	people := make([]uuid.UUID, 0, len(lists))
	for _, list := range lists {
		ids = append(ids, list.ID)
		people = append(people, list.OwnerAuthID)
	}
	members, err := s.repo.Members(ctx, ids)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.ItemCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	lastDraws, err := s.repo.LastDraws(ctx, ids)
	if err != nil {
		return nil, err
	}
	draws := make([]sharedlist.Draw, 0, len(lastDraws))
	for _, draw := range lastDraws {
		draws = append(draws, draw)
	}
	drawViews, err := s.drawViews(ctx, draws)
	if err != nil {
		return nil, err
	}
	viewByList := map[uint]SharedListDraw{}
	for i, draw := range draws {
		viewByList[draw.ListID] = drawViews[i]
	}
	names, err := s.repo.Names(ctx, people)
	if err != nil {
		return nil, err
	}

	dropped := map[uuid.UUID][]uuid.UUID{}
	for _, list := range lists {
		visible, blocked := states[list.OwnerAuthID].visible(members[list.ID])
		for _, member := range blocked {
			if !slices.Contains(dropped[list.OwnerAuthID], member) {
				dropped[list.OwnerAuthID] = append(dropped[list.OwnerAuthID], member)
			}
		}
		summary := summaryOf(list, caller, names, len(visible), counts[list.ID])
		if view, ok := viewByList[list.ID]; ok {
			summary.LastDraw = &view
		}
		result = append(result, summary)
	}
	for owner, members := range dropped {
		s.dropBlocked(ctx, owner, members)
	}
	return result, nil
}

func summaryOf(list sharedlist.List, caller uuid.UUID, names map[uuid.UUID]string, members, items int) SharedListSummary {
	return SharedListSummary{
		ID:           list.ID,
		Title:        list.Title,
		Owner:        userRef(list.OwnerAuthID, names),
		IsOwner:      list.OwnerAuthID == caller,
		MembersCount: members,
		ItemsCount:   items,
		CreatedAt:    list.CreatedAt.UTC(),
		UpdatedAt:    list.UpdatedAt.UTC(),
	}
}

func (s *SharedListService) card(ctx context.Context, list sharedlist.List, caller uuid.UUID) (SharedListCard, error) {
	membersByList, err := s.repo.Members(ctx, []uint{list.ID})
	if err != nil {
		return SharedListCard{}, err
	}
	members, blocked := s.ownerBlocks(ctx, list.OwnerAuthID).visible(membersByList[list.ID])
	s.dropBlocked(ctx, list.OwnerAuthID, blocked)
	items, err := s.repo.Items(ctx, list.ID)
	if err != nil {
		return SharedListCard{}, err
	}
	draws, err := s.repo.Draws(ctx, list.ID, cardDraws)
	if err != nil {
		return SharedListCard{}, err
	}
	drawViews, err := s.drawViews(ctx, draws)
	if err != nil {
		return SharedListCard{}, err
	}

	filmIDs := make([]uint, 0, len(items))
	people := append([]uuid.UUID{list.OwnerAuthID}, members...)
	for _, item := range items {
		filmIDs = append(filmIDs, item.FilmID)
		people = append(people, item.AddedByAuthID)
	}
	films, err := s.filmItems(ctx, filmIDs)
	if err != nil {
		return SharedListCard{}, err
	}
	names, err := s.repo.Names(ctx, people)
	if err != nil {
		return SharedListCard{}, err
	}

	card := SharedListCard{
		SharedListSummary: summaryOf(list, caller, names, len(members), 0),
		Members:           make([]UserRef, 0, len(members)),
		Items:             make([]SharedListItem, 0, len(items)),
		Draws:             drawViews,
	}
	for _, member := range members {
		card.Members = append(card.Members, userRef(member, names))
	}
	for _, item := range items {
		filmView, ok := films[item.FilmID]
		if !ok {
			continue
		}
		card.Items = append(card.Items, SharedListItem{
			Film:    filmView,
			AddedBy: userRef(item.AddedByAuthID, names),
			AddedAt: item.AddedAt.UTC(),
		})
	}
	card.ItemsCount = len(card.Items)
	if len(drawViews) > 0 {
		card.LastDraw = &drawViews[0]
	}
	return card, nil
}

func (s *SharedListService) drawViews(ctx context.Context, draws []sharedlist.Draw) ([]SharedListDraw, error) {
	views := make([]SharedListDraw, 0, len(draws))
	if len(draws) == 0 {
		return views, nil
	}
	var filmIDs []uint
	people := make([]uuid.UUID, 0, len(draws))
	for _, draw := range draws {
		filmIDs = append(filmIDs, draw.FilmIDs...)
		people = append(people, draw.DrawnByAuthID)
	}
	films, err := s.filmItems(ctx, filmIDs)
	if err != nil {
		return nil, err
	}
	names, err := s.repo.Names(ctx, people)
	if err != nil {
		return nil, err
	}
	for _, draw := range draws {
		view := SharedListDraw{
			ID:        draw.ID,
			Mode:      string(draw.Mode),
			Count:     draw.Count,
			Films:     make([]FilmSearchItem, 0, len(draw.FilmIDs)),
			DrawnBy:   userRef(draw.DrawnByAuthID, names),
			CreatedAt: draw.CreatedAt.UTC(),
		}
		for _, id := range draw.FilmIDs {
			if item, ok := films[id]; ok {
				view.Films = append(view.Films, item)
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SharedListService) filmItems(ctx context.Context, ids []uint) (map[uint]FilmSearchItem, error) {
	result := map[uint]FilmSearchItem{}
	if len(ids) == 0 {
		return result, nil
	}
	films, err := s.repo.Films(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, item := range catalogItems(films) {
		result[item.ID] = item
	}
	return result, nil
}

func userRef(id uuid.UUID, names map[uuid.UUID]string) UserRef {
	ref := UserRef{AuthUserID: id}
	if name, ok := names[id]; ok && name != "" && name != id.String() {
		ref.Name = &name
	}
	return ref
}
