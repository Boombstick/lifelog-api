package authsvc_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

type mergeRepo struct {
	authsvc.Repository

	byID       map[uuid.UUID]*authsvc.User
	byTelegram map[int64]*authsvc.User
	nonces     map[string]*authsvc.LinkNonce

	created   []*authsvc.User
	linked    map[uuid.UUID]int64
	merges    []authsvc.Merge
	completed []uuid.UUID
}

func newMergeRepo(users ...*authsvc.User) *mergeRepo {
	repo := &mergeRepo{
		byID:       map[uuid.UUID]*authsvc.User{},
		byTelegram: map[int64]*authsvc.User{},
		nonces:     map[string]*authsvc.LinkNonce{},
		linked:     map[uuid.UUID]int64{},
	}
	for _, user := range users {
		repo.byID[user.ID] = user
	}
	return repo
}

func (r *mergeRepo) UserByID(_ context.Context, id uuid.UUID) (*authsvc.User, error) {
	if user, ok := r.byID[id]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *mergeRepo) UserByTelegram(_ context.Context, telegramID int64) (*authsvc.User, error) {
	if user, ok := r.byTelegram[telegramID]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *mergeRepo) CreateUser(_ context.Context, user *authsvc.User) error {
	r.created = append(r.created, user)
	r.byID[user.ID] = user
	return nil
}

func (r *mergeRepo) LinkTelegram(_ context.Context, userID uuid.UUID, telegramID int64) error {
	r.linked[userID] = telegramID
	r.byTelegram[telegramID] = r.byID[userID]
	return nil
}

func (r *mergeRepo) TelegramIDs(_ context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	for telegramID, user := range r.byTelegram {
		for _, id := range userIDs {
			if user.ID == id {
				out[id] = telegramID
			}
		}
	}
	return out, nil
}

func (r *mergeRepo) ReplaceNonce(_ context.Context, nonce *authsvc.LinkNonce) error {
	for code, old := range r.nonces {
		if old.UserID == nonce.UserID {
			delete(r.nonces, code)
		}
	}
	r.nonces[nonce.Code] = nonce
	return nil
}

func (r *mergeRepo) ConsumeNonce(_ context.Context, code string) (*authsvc.LinkNonce, error) {
	nonce, ok := r.nonces[code]
	if !ok {
		return nil, authsvc.ErrNonceInvalid
	}
	delete(r.nonces, code)
	return nonce, nil
}

func (r *mergeRepo) AbsorbSilent(_ context.Context, silent, into uuid.UUID) (*authsvc.Merge, error) {
	source, ok := r.byID[silent]
	if !ok || !source.IsSilent() {
		return nil, authsvc.ErrTelegramTaken
	}
	for _, user := range r.byTelegram {
		if user.ID == into {
			return nil, authsvc.ErrTelegramAlreadyLinked
		}
	}
	for telegramID, user := range r.byTelegram {
		if user.ID == silent {
			r.byTelegram[telegramID] = r.byID[into]
			r.linked[into] = telegramID
			delete(r.linked, silent)
		}
	}
	delete(r.byID, silent)

	merge := authsvc.Merge{ID: uuid.New(), FromUser: silent, IntoUser: into, Status: authsvc.MergeStatusPending}
	r.merges = append(r.merges, merge)
	return &merge, nil
}

func (r *mergeRepo) PendingMerges(context.Context) ([]authsvc.Merge, error) {
	var pending []authsvc.Merge
	for _, merge := range r.merges {
		if !slices.Contains(r.completed, merge.ID) {
			pending = append(pending, merge)
		}
	}
	return pending, nil
}

func (r *mergeRepo) CompleteMerge(_ context.Context, id uuid.UUID) error {
	r.completed = append(r.completed, id)
	return nil
}

func userCreatedAt(login string, at time.Time) *authsvc.User {
	name := login
	return &authsvc.User{ID: uuid.New(), Login: &name, DisplayName: login, Role: authsvc.RoleUser, CreatedAt: at}
}

func silentUser(at time.Time) *authsvc.User {
	return &authsvc.User{ID: uuid.New(), DisplayName: "tg", Role: authsvc.RoleUser, CreatedAt: at}
}

func TestТихаяБотУчёткаСливаетсяВУчёткуПриложения(t *testing.T) {
	for _, botOlder := range []bool{true, false} {
		appAt, botAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		if botOlder {
			appAt, botAt = botAt, appAt
		}
		app := userCreatedAt("masha", appAt)
		bot := silentUser(botAt)
		repo := newMergeRepo(app, bot)
		repo.byTelegram[777] = bot
		repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: app.ID}

		survivor, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 777)
		if err != nil {
			t.Fatalf("бот старше=%v: привязка: %v", botOlder, err)
		}
		if survivor.ID != app.ID {
			t.Errorf("бот старше=%v: выжила %v, ожидалась учётка приложения", botOlder, survivor.ID)
		}
		if len(repo.merges) != 1 || repo.merges[0].FromUser != bot.ID || repo.merges[0].IntoUser != app.ID {
			t.Errorf("бот старше=%v: слияния %+v", botOlder, repo.merges)
		}
		if len(repo.completed) != 1 {
			t.Errorf("бот старше=%v: слияние не закрыто", botOlder)
		}
		if _, err := repo.UserByID(context.Background(), bot.ID); !errors.Is(err, authsvc.ErrUserNotFound) {
			t.Errorf("бот старше=%v: тихая учётка осталась: %v", botOlder, err)
		}
		if owner, err := repo.UserByTelegram(context.Background(), 777); err != nil || owner.ID != app.ID {
			t.Errorf("бот старше=%v: телеграм ведёт в %v (%v), ожидалась учётка приложения", botOlder, owner, err)
		}
	}
}

type mergeNotifier struct {
	recordingNotifier
	sent [][2]uuid.UUID
	fail int
}

func (n *mergeNotifier) NotifyMerge(_ context.Context, from, into uuid.UUID) error {
	if n.fail > 0 {
		n.fail--
		return errors.New("trips down")
	}
	n.sent = append(n.sent, [2]uuid.UUID{from, into})
	return nil
}

func TestОборванноеСлияниеДоигрываетсяТемиЖеИдентификаторами(t *testing.T) {
	app := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bot := silentUser(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(app, bot)
	repo.byTelegram[777] = bot
	repo.linked[bot.ID] = 777
	repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: app.ID}
	notifier := &mergeNotifier{fail: 1}
	service := serviceWithNotifier(repo, notifier)

	if _, err := service.ConfirmTelegramLink(context.Background(), "code", 777); err == nil {
		t.Fatal("рассылка упала, а привязка ответила успехом")
	}
	if len(repo.completed) != 0 {
		t.Fatal("слияние закрыто, хотя поездки его не приняли")
	}
	if _, ok := repo.byID[bot.ID]; ok {
		t.Error("тихая учётка пережила слияние")
	}
	if owner, err := repo.UserByTelegram(context.Background(), 777); err != nil || owner.ID != app.ID {
		t.Errorf("телеграм ведёт в %v (%v), ожидалась учётка приложения", owner, err)
	}
	if _, ok := repo.linked[bot.ID]; ok {
		t.Error("у удалённой учётки осталась привязка")
	}

	if err := service.ResumeMerges(context.Background()); err != nil {
		t.Fatalf("доигрывание: %v", err)
	}
	if err := service.ResumeMerges(context.Background()); err != nil {
		t.Fatalf("повторное доигрывание: %v", err)
	}
	want := [][2]uuid.UUID{{bot.ID, app.ID}}
	if !slices.Equal(notifier.sent, want) {
		t.Errorf("разослано %v, ожидалось %v", notifier.sent, want)
	}
	if len(repo.completed) != 1 || repo.completed[0] != repo.merges[0].ID {
		t.Errorf("закрыто %v, ожидалось слияние %v", repo.completed, repo.merges[0].ID)
	}
}

func TestРепозиторийНеПоглощаетНеТихуюУчётку(t *testing.T) {
	app := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	other := userCreatedAt("petya", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(app, other)

	stale := *other
	stale.Login = nil
	repo.byTelegram[777] = &stale
	repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: app.ID}

	_, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 777)
	if !errors.Is(err, authsvc.ErrTelegramTaken) {
		t.Errorf("ошибка %v, ожидалась ErrTelegramTaken", err)
	}
	if _, ok := repo.byID[other.ID]; !ok {
		t.Error("настоящая учётка удалена")
	}
	if len(repo.merges) != 0 {
		t.Errorf("записано слияние %+v", repo.merges)
	}
}

func TestЗанятыйНастоящейУчёткойТелеграмОтклоняется(t *testing.T) {
	app := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	for name, other := range map[string]*authsvc.User{
		"с логином": userCreatedAt("petya", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		"с почтой": func() *authsvc.User {
			user := silentUser(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
			email := "petya@example.com"
			user.Email = &email
			return user
		}(),
	} {
		repo := newMergeRepo(app, other)
		repo.byTelegram[777] = other
		repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: app.ID}

		_, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 777)
		if !errors.Is(err, authsvc.ErrTelegramTaken) {
			t.Errorf("%s: ошибка %v, ожидалась ErrTelegramTaken", name, err)
		}
		if len(repo.merges) != 0 {
			t.Errorf("%s: записано слияние %+v", name, repo.merges)
		}
	}
}

func TestУчёткаСТелеграмомВторойНеПривязывает(t *testing.T) {
	app := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(app)
	repo.byTelegram[555] = app

	if _, err := newService(repo).StartTelegramLink(context.Background(), app.ID); !errors.Is(err, authsvc.ErrTelegramAlreadyLinked) {
		t.Errorf("выдача кода: %v, ожидалась ErrTelegramAlreadyLinked", err)
	}

	repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: app.ID}
	if _, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 999); !errors.Is(err, authsvc.ErrTelegramAlreadyLinked) {
		t.Errorf("подтверждение: %v, ожидалась ErrTelegramAlreadyLinked", err)
	}
	if repo.linked[app.ID] != 0 {
		t.Errorf("учётку перепривязали на %d", repo.linked[app.ID])
	}
}

func TestНовыйКодГаситПрежний(t *testing.T) {
	app := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(app)
	service := newService(repo)

	first, err := service.StartTelegramLink(context.Background(), app.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartTelegramLink(context.Background(), app.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := repo.nonces[first.Code]; ok {
		t.Error("первый код остался рабочим")
	}
	if _, ok := repo.nonces[second.Code]; !ok {
		t.Error("второго кода нет")
	}
	if ttl := second.Expires.Sub(time.Now()); ttl > 3*time.Minute || ttl < 2*time.Minute {
		t.Errorf("код живёт %v, ожидалось 3 минуты", ttl)
	}
}

func TestПовторнаяПривязкаСвоегоЖеТелеграмаНеСливает(t *testing.T) {
	user := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(user)
	repo.byTelegram[555] = user
	repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: user.ID}

	got, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 555)
	if err != nil {
		t.Fatalf("привязка: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("вернулась чужая учётка %v", got.ID)
	}
	if len(repo.merges) != 0 {
		t.Errorf("слияние записано на пустом месте: %+v", repo.merges)
	}
}

func TestСвободныйТелеграмПривязываетсяБезСлияния(t *testing.T) {
	user := userCreatedAt("masha", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	repo := newMergeRepo(user)
	repo.nonces["code"] = &authsvc.LinkNonce{Code: "code", UserID: user.ID}

	if _, err := newService(repo).ConfirmTelegramLink(context.Background(), "code", 42); err != nil {
		t.Fatalf("привязка: %v", err)
	}
	if repo.linked[user.ID] != 42 {
		t.Errorf("телеграм не привязан: %v", repo.linked)
	}
	if len(repo.merges) != 0 {
		t.Errorf("слияние записано на пустом месте: %+v", repo.merges)
	}
}

func TestУчёткаИзБотаПолучаетИмяПоУмолчанию(t *testing.T) {
	repo := newMergeRepo()

	user, err := newService(repo).EnsureTelegramUser(context.Background(), 12345, "")
	if err != nil {
		t.Fatalf("заведение учётки: %v", err)
	}
	if user.DisplayName != "tg-12345" {
		t.Errorf("имя %q, ожидалось \"tg-12345\"", user.DisplayName)
	}
	if repo.linked[user.ID] != 12345 {
		t.Errorf("телеграм не привязан к новой учётке: %v", repo.linked)
	}

	named, err := newService(repo).EnsureTelegramUser(context.Background(), 54321, "Маша")
	if err != nil {
		t.Fatalf("заведение учётки: %v", err)
	}
	if named.DisplayName != "Маша" {
		t.Errorf("имя %q, ожидалось \"Маша\"", named.DisplayName)
	}

	spaced, err := newService(repo).EnsureTelegramUser(context.Background(), 777, "  Маша   К. ")
	if err != nil {
		t.Fatalf("заведение учётки: %v", err)
	}
	if spaced.DisplayName != "Маша К." {
		t.Errorf("имя %q, ожидалось \"Маша К.\"", spaced.DisplayName)
	}
	hidden, err := newService(repo).EnsureTelegramUser(context.Background(), 778, "Ма"+string(rune(0x202e))+"ша")
	if err != nil {
		t.Fatalf("заведение учётки: %v", err)
	}
	if hidden.DisplayName != "tg-778" {
		t.Errorf("имя %q, ожидалось \"tg-778\"", hidden.DisplayName)
	}
}
