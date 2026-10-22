package authsvc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

type subscriptionKey struct {
	follower uuid.UUID
	target   uuid.UUID
}

type subscriptionsRepo struct {
	authsvc.Repository

	users map[string]*authsvc.User
	rows  map[subscriptionKey]*authsvc.Subscription
}

func newSubscriptionsRepo(users ...*authsvc.User) *subscriptionsRepo {
	repo := &subscriptionsRepo{
		users: make(map[string]*authsvc.User, len(users)),
		rows:  map[subscriptionKey]*authsvc.Subscription{},
	}
	for _, user := range users {
		repo.users[*user.Login] = user
	}
	return repo
}

func newUser(login string, private bool) *authsvc.User {
	name := login
	return &authsvc.User{ID: uuid.New(), Login: &name, DisplayName: login, IsPrivate: private}
}

func (r *subscriptionsRepo) UserByLogin(_ context.Context, login string) (*authsvc.User, error) {
	user, ok := r.users[login]
	if !ok {
		return nil, authsvc.ErrUserNotFound
	}
	return user, nil
}

func (r *subscriptionsRepo) Subscribe(_ context.Context, subscription *authsvc.Subscription) error {
	key := subscriptionKey{subscription.FollowerID, subscription.TargetID}
	if _, exists := r.rows[key]; exists {
		return nil
	}
	stored := *subscription
	r.rows[key] = &stored
	return nil
}

func (r *subscriptionsRepo) Unsubscribe(_ context.Context, followerID, targetID uuid.UUID) error {
	delete(r.rows, subscriptionKey{followerID, targetID})
	return nil
}

func (r *subscriptionsRepo) SetSubscriptionStatus(_ context.Context, followerID, targetID uuid.UUID, status string) error {
	row, ok := r.rows[subscriptionKey{followerID, targetID}]
	if !ok {
		return authsvc.ErrSubscriptionAbsent
	}
	decided := time.Now()
	row.Status = status
	row.DecidedAt = &decided
	return nil
}

func (r *subscriptionsRepo) SubscriptionsOf(_ context.Context, followerID uuid.UUID) ([]authsvc.SubscriptionView, error) {
	views := make([]authsvc.SubscriptionView, 0, len(r.rows))
	for key, row := range r.rows {
		if key.follower == followerID {
			views = append(views, r.view(key.target, row))
		}
	}
	return views, nil
}

func (r *subscriptionsRepo) SubscribersOf(_ context.Context, targetID uuid.UUID) ([]authsvc.SubscriptionView, error) {
	views := make([]authsvc.SubscriptionView, 0, len(r.rows))
	for key, row := range r.rows {
		if key.target == targetID {
			views = append(views, r.view(key.follower, row))
		}
	}
	return views, nil
}

func (r *subscriptionsRepo) SetPrivate(_ context.Context, userID uuid.UUID, isPrivate bool) error {
	user := r.userByID(userID)
	if user == nil {
		return authsvc.ErrUserNotFound
	}
	user.IsPrivate = isPrivate
	return nil
}

func (r *subscriptionsRepo) view(id uuid.UUID, row *authsvc.Subscription) authsvc.SubscriptionView {
	view := authsvc.SubscriptionView{UserID: id, Status: row.Status, CreatedAt: row.CreatedAt}
	if user := r.userByID(id); user != nil {
		view.Login = *user.Login
		view.DisplayName = user.DisplayName
	}
	return view
}

func (r *subscriptionsRepo) userByID(id uuid.UUID) *authsvc.User {
	for _, user := range r.users {
		if user.ID == id {
			return user
		}
	}
	return nil
}

func (r *subscriptionsRepo) statusOf(t *testing.T, follower, target uuid.UUID) string {
	t.Helper()
	row, ok := r.rows[subscriptionKey{follower, target}]
	if !ok {
		t.Fatalf("подписки %s → %s нет", follower, target)
	}
	return row.Status
}

func TestПодпискаНаСебяОтвергается(t *testing.T) {
	self := newUser("follower", false)
	repo := newSubscriptionsRepo(self)
	service := newService(repo)

	_, err := service.Subscribe(context.Background(), self.ID, "  FOLLOWER  ")
	if !errors.Is(err, authsvc.ErrSelfSubscription) {
		t.Fatalf("ошибка: %v, ожидалась ErrSelfSubscription", err)
	}
	if len(repo.rows) != 0 {
		t.Errorf("записано %d подписок, ожидалось 0", len(repo.rows))
	}
}

func TestПодпискаНаНесуществующийЛогин(t *testing.T) {
	repo := newSubscriptionsRepo()
	service := newService(repo)

	_, err := service.Subscribe(context.Background(), uuid.New(), "никого")
	if !errors.Is(err, authsvc.ErrUserNotFound) {
		t.Fatalf("ошибка: %v, ожидалась ErrUserNotFound", err)
	}
}

func TestПодпискаНаОткрытыйПрофильСразуАктивна(t *testing.T) {
	follower := newUser("follower", false)
	target := newUser("target", false)
	repo := newSubscriptionsRepo(follower, target)
	service := newService(repo)

	view, err := service.Subscribe(context.Background(), follower.ID, "target")
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if view.Status != authsvc.SubscriptionActive {
		t.Errorf("статус %q, ожидался %q", view.Status, authsvc.SubscriptionActive)
	}
	if view.UserID != target.ID || view.Login != "target" {
		t.Errorf("ответ указывает не на того человека: %+v", view)
	}
	if got := repo.statusOf(t, follower.ID, target.ID); got != authsvc.SubscriptionActive {
		t.Errorf("в базе статус %q, ожидался %q", got, authsvc.SubscriptionActive)
	}
}

func TestПодпискаНаЗакрытыйПрофильЖдётОдобрения(t *testing.T) {
	follower := newUser("follower", false)
	target := newUser("target", true)
	repo := newSubscriptionsRepo(follower, target)
	service := newService(repo)

	view, err := service.Subscribe(context.Background(), follower.ID, "target")
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if view.Status != authsvc.SubscriptionPending {
		t.Errorf("статус %q, ожидался %q", view.Status, authsvc.SubscriptionPending)
	}

	subscribers, err := service.Subscribers(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("список подписчиков: %v", err)
	}
	if len(subscribers) != 1 || subscribers[0].UserID != follower.ID {
		t.Fatalf("подписчики закрытого профиля: %+v", subscribers)
	}
	if subscribers[0].Status != authsvc.SubscriptionPending {
		t.Errorf("владелец видит заявку со статусом %q", subscribers[0].Status)
	}
}

func TestПовторнаяПодпискаНеСбрасываетОдобрение(t *testing.T) {
	follower := newUser("follower", false)
	target := newUser("target", true)
	repo := newSubscriptionsRepo(follower, target)
	service := newService(repo)

	if _, err := service.Subscribe(context.Background(), follower.ID, "target"); err != nil {
		t.Fatalf("первая подписка: %v", err)
	}
	if err := service.ApproveSubscriber(context.Background(), target.ID, follower.ID); err != nil {
		t.Fatalf("одобрение: %v", err)
	}

	view, err := service.Subscribe(context.Background(), follower.ID, "target")
	if err != nil {
		t.Fatalf("повторная подписка: %v", err)
	}
	if view.Status != authsvc.SubscriptionActive {
		t.Errorf("повтор вернул статус %q, ожидался %q", view.Status, authsvc.SubscriptionActive)
	}
	if got := repo.statusOf(t, follower.ID, target.ID); got != authsvc.SubscriptionActive {
		t.Errorf("повтор сбросил статус в базе на %q", got)
	}
	if len(repo.rows) != 1 {
		t.Errorf("повтор завёл вторую строку: %d", len(repo.rows))
	}
}

func TestОдобрениеНесуществующейЗаявки(t *testing.T) {
	target := newUser("target", true)
	repo := newSubscriptionsRepo(target)
	service := newService(repo)

	err := service.ApproveSubscriber(context.Background(), target.ID, uuid.New())
	if !errors.Is(err, authsvc.ErrSubscriptionAbsent) {
		t.Fatalf("ошибка: %v, ожидалась ErrSubscriptionAbsent", err)
	}
}

func TestОтклонениеУдаляетЗаявку(t *testing.T) {
	follower := newUser("follower", false)
	target := newUser("target", true)
	repo := newSubscriptionsRepo(follower, target)
	service := newService(repo)

	if _, err := service.Subscribe(context.Background(), follower.ID, "target"); err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if err := service.DeclineSubscriber(context.Background(), target.ID, follower.ID); err != nil {
		t.Fatalf("отклонение: %v", err)
	}

	if len(repo.rows) != 0 {
		t.Fatalf("после отклонения осталось %d строк", len(repo.rows))
	}
	subscribers, err := service.Subscribers(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("список подписчиков: %v", err)
	}
	if len(subscribers) != 0 {
		t.Errorf("отклонённый остался в подписчиках: %+v", subscribers)
	}

	view, err := service.Subscribe(context.Background(), follower.ID, "target")
	if err != nil {
		t.Fatalf("повторная заявка после отказа: %v", err)
	}
	if view.Status != authsvc.SubscriptionPending {
		t.Errorf("повторная заявка получила статус %q, ожидался %q", view.Status, authsvc.SubscriptionPending)
	}
}

func TestОтпискаУбираетИзОбоихСписков(t *testing.T) {
	follower := newUser("follower", false)
	target := newUser("target", false)
	repo := newSubscriptionsRepo(follower, target)
	service := newService(repo)

	if _, err := service.Subscribe(context.Background(), follower.ID, "target"); err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if err := service.Unsubscribe(context.Background(), follower.ID, target.ID); err != nil {
		t.Fatalf("отписка: %v", err)
	}

	subscriptions, err := service.Subscriptions(context.Background(), follower.ID)
	if err != nil {
		t.Fatalf("список подписок: %v", err)
	}
	if len(subscriptions) != 0 {
		t.Errorf("подписка осталась у подписавшегося: %+v", subscriptions)
	}

	subscribers, err := service.Subscribers(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("список подписчиков: %v", err)
	}
	if len(subscribers) != 0 {
		t.Errorf("подписчик остался у цели: %+v", subscribers)
	}
}

func TestЗакрытиеПрофиляНеТрогаетОдобренные(t *testing.T) {
	old := newUser("старый", false)
	fresh := newUser("новый", false)
	target := newUser("target", false)
	repo := newSubscriptionsRepo(old, fresh, target)
	service := newService(repo)

	if _, err := service.Subscribe(context.Background(), old.ID, "target"); err != nil {
		t.Fatalf("подписка до закрытия: %v", err)
	}
	if err := service.SetPrivate(context.Background(), target.ID, true); err != nil {
		t.Fatalf("закрытие профиля: %v", err)
	}

	if got := repo.statusOf(t, old.ID, target.ID); got != authsvc.SubscriptionActive {
		t.Errorf("закрытие профиля сбросило прежнюю подписку в %q", got)
	}

	view, err := service.Subscribe(context.Background(), fresh.ID, "target")
	if err != nil {
		t.Fatalf("подписка после закрытия: %v", err)
	}
	if view.Status != authsvc.SubscriptionPending {
		t.Errorf("новая подписка на закрытый профиль получила статус %q", view.Status)
	}

	if err := service.SetPrivate(context.Background(), target.ID, false); err != nil {
		t.Fatalf("открытие профиля: %v", err)
	}
	if got := repo.statusOf(t, fresh.ID, target.ID); got != authsvc.SubscriptionPending {
		t.Errorf("открытие профиля само одобрило заявку: %q", got)
	}
}

func (r *subscriptionsRepo) BlockState(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) {
	return false, false, nil
}
