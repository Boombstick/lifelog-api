package trips

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newPhotoFixture() (*memStore, *fakeFiles, *PhotoService, uuid.UUID) {
	store := seededStore()
	files := newFakeFiles()
	return store, files, NewPhotoService(store, files, fixedClock{testNow}), uuid.New()
}

func photoCmd(mutate func(*AddPhotoCommand)) AddPhotoCommand {
	cmd := AddPhotoCommand{FileName: "photo.png", ContentType: "image/png", Size: 4, Body: strings.NewReader("\x89PNG")}
	if mutate != nil {
		mutate(&cmd)
	}
	return cmd
}

func TestPhotoAddChecksInOrder(t *testing.T) {
	store, files, svc, user := newPhotoFixture()
	cases := []struct {
		name string
		cmd  AddPhotoCommand
		kind Kind
		msg  string
	}{
		{"empty file", photoCmd(func(c *AddPhotoCommand) { c.Size = 0; c.ContentType = "text/plain" }), KindValidation, "Файл не передан"},
		{"negative size", photoCmd(func(c *AddPhotoCommand) { c.Size = -1 }), KindValidation, "Файл пустой"},
		{"over 15 MiB", photoCmd(func(c *AddPhotoCommand) { c.Size = MaxPhotoBytes + 1; c.ContentType = "text/plain" }), KindValidation, "Файл больше 15 МБ"},
		{"unsupported type before trip lookup", photoCmd(func(c *AddPhotoCommand) { c.ContentType = "image/gif" }), KindValidation, "Поддерживаются только JPEG, PNG, WebP и HEIC"},
		{"no active trip", photoCmd(nil), KindValidation, "У вас нет активной поездки"},
		{"all-zero trip id is the active trip", photoCmd(func(c *AddPhotoCommand) { c.TripID = ptr(uuid.Nil) }), KindValidation, "У вас нет активной поездки"},
		{"unknown trip", photoCmd(func(c *AddPhotoCommand) { c.TripID = ptr(uuid.New()) }), KindNotFound, "Поездка не найдена"},
	}
	for _, c := range cases {
		_, err := svc.Add(bg, appActor(user), c.cmd)
		if e := mustErr(err); e == nil || e.Kind != c.kind || e.Message != c.msg {
			t.Errorf("%s: got %v", c.name, err)
		}
	}

	store.putTrip(Trip{UserID: user, Name: "Asia"})
	_, err := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.Caption = ptr(strings.Repeat("я", 501)) }))
	wantErr(t, err, KindValidation, "Поле caption не может быть длиннее 500 символов")
	_, err = svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.FileName = "dir/" + strings.Repeat("a", 256) }))
	wantErr(t, err, KindValidation, "Поле file не может быть длиннее 255 символов")

	if _, err := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.Size = MaxPhotoBytes })); err != nil {
		t.Fatalf("exactly 15 MiB rejected: %v", err)
	}
	if len(files.files) != 1 || store.calls["InsertPhoto"] != 1 {
		t.Fatalf("failed checks wrote files %d rows %d", len(files.files), store.calls["InsertPhoto"])
	}
}

func TestPhotoAdd(t *testing.T) {
	store, files, svc, user := newPhotoFixture()
	trip := store.putTrip(Trip{UserID: user, Name: "Asia"})

	got, err := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) {
		c.ContentType = "IMAGE/JPEG"
		c.FileName = "/sdcard/DCIM/пляж.jpg"
		c.Caption = ptr("Пляж")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.TripID != trip.ID || *got.FileName != "пляж.jpg" || *got.Caption != "Пляж" || !got.CreatedAt.Equal(testNow) || got.ID.Version() != 4 {
		t.Fatalf("response %+v", got)
	}
	photo := store.photos[got.ID]
	if !regexp.MustCompile(`^[0-9a-f]{32}\.jpg$`).MatchString(photo.StoredFileName) {
		t.Fatalf("stored name %q", photo.StoredFileName)
	}
	if photo.ContentType != "IMAGE/JPEG" || photo.SizeBytes != 4 || string(files.files[photo.StoredFileName]) != "\x89PNG" {
		t.Fatalf("row %+v", photo)
	}

	extensions := map[string]string{"image/png": ".png", "image/webp": ".webp", "Image/HEIC": ".heic"}
	for contentType, ext := range extensions {
		got, err := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.ContentType = contentType; c.FileName = "" }))
		if err != nil {
			t.Fatal(err)
		}
		if name := store.photos[got.ID].StoredFileName; !strings.HasSuffix(name, ext) {
			t.Errorf("%s: %s", contentType, name)
		}

		if *got.FileName != "" || got.Caption != nil {
			t.Errorf("%s: %+v", contentType, got)
		}
	}

	win, _ := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.FileName = `C:\x\a.png`; c.Caption = ptr("") }))
	if *win.FileName != `C:\x\a.png` || win.Caption != nil {
		t.Fatalf("windows name %+v", win)
	}

	done := store.putTrip(Trip{UserID: user, Name: "Old", Status: StatusCompleted})
	if got, err := svc.Add(bg, appActor(user), photoCmd(func(c *AddPhotoCommand) { c.TripID = &done.ID })); err != nil || got.TripID != done.ID {
		t.Fatalf("completed trip: %+v %v", got, err)
	}

	_, err = svc.Add(bg, appActor(uuid.New()), photoCmd(func(c *AddPhotoCommand) { c.TripID = &trip.ID }))
	wantErr(t, err, KindNotFound, "Поездка не найдена")
}

func TestPhotoAddFileFailureWritesNoRow(t *testing.T) {
	store, files, svc, user := newPhotoFixture()
	store.putTrip(Trip{UserID: user})
	files.saveErr = errors.New("disk full")
	if _, err := svc.Add(bg, appActor(user), photoCmd(nil)); !errors.Is(err, files.saveErr) {
		t.Fatalf("got %v", err)
	}
	if store.calls["InsertPhoto"] != 0 {
		t.Fatal("row written without a file")
	}
}

func TestPhotoList(t *testing.T) {
	store, _, svc, user := newPhotoFixture()
	_, err := svc.List(bg, appActor(user), nil)
	wantErr(t, err, KindValidation, "У вас нет активной поездки")

	trip := store.putTrip(Trip{UserID: user, Name: "Asia"})
	older := Photo{ID: uuid.New(), TripID: trip.ID, OriginalFileName: ptr("a.png"), SizeBytes: 82, CreatedAt: testNow.Add(-time.Hour)}
	newer := Photo{ID: uuid.New(), TripID: trip.ID, Caption: ptr("Пляж"), CreatedAt: testNow}
	store.photos[older.ID], store.photos[newer.ID] = older, newer

	got, err := svc.List(bg, appActor(user), nil)
	if err != nil || len(got.Result) != 2 || got.Result[0].ID != newer.ID || got.Result[0].FileName != nil || *got.Result[1].FileName != "a.png" || got.Result[1].SizeBytes != 82 {
		t.Fatalf("got %+v %v", got, err)
	}
	byZero, _ := svc.List(bg, appActor(user), ptr(uuid.Nil))
	if len(byZero.Result) != 2 {
		t.Fatal("all-zero id must mean the active trip")
	}

	stranger, err := svc.List(bg, appActor(uuid.New()), &trip.ID)
	if err != nil || stranger.Result == nil || len(stranger.Result) != 0 {
		t.Fatalf("someone else's trip must be [], got %+v %v", stranger, err)
	}

	store.putTrip(Trip{UserID: user, Name: "Second"})
	if _, err := svc.List(bg, appActor(user), nil); err != nil {
		t.Fatalf("two active trips: %v", err)
	}
}

func TestPhotoOpen(t *testing.T) {
	store, files, svc, user := newPhotoFixture()
	trip := store.putTrip(Trip{UserID: user})
	photo := Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: "abc.png", ContentType: "image/png", OriginalFileName: ptr("photo.png")}
	store.photos[photo.ID] = photo

	_, err := svc.Open(bg, appActor(uuid.New()), photo.ID)
	wantErr(t, err, KindNotFound, "Фотография не найдена")
	_, err = svc.Open(bg, appActor(user), photo.ID)
	wantErr(t, err, KindNotFound, "Файл фотографии потерян")

	files.files["abc.png"] = []byte("data")
	got, err := svc.Open(bg, appActor(user), photo.ID)
	if err != nil || got.ContentType != "image/png" || got.FileName != "photo.png" {
		t.Fatalf("got %+v %v", got, err)
	}
	body, _ := io.ReadAll(got.Body)
	_ = got.Body.Close()
	if string(body) != "data" {
		t.Fatalf("body %q", body)
	}

	photo.OriginalFileName = nil
	store.photos[photo.ID] = photo
	got, _ = svc.Open(bg, appActor(user), photo.ID)
	if got.FileName != "" {
		t.Fatalf("null name %q", got.FileName)
	}
}

func TestPhotoDelete(t *testing.T) {
	store, files, svc, user := newPhotoFixture()
	trip := store.putTrip(Trip{UserID: user})
	photo := Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: "abc.png"}
	store.photos[photo.ID] = photo
	files.files["abc.png"] = []byte("data")
	files.log = &store.log

	wantErr(t, svc.Delete(bg, appActor(uuid.New()), photo.ID), KindNotFound, "Фотография не найдена")
	if err := svc.Delete(bg, appActor(user), photo.ID); err != nil {
		t.Fatal(err)
	}
	if last := store.log[len(store.log)-2:]; last[0] != "DeletePhoto" || last[1] != "file:Delete" {
		t.Fatalf("order %v", last)
	}
	if _, ok := files.files["abc.png"]; ok {
		t.Fatal("file kept")
	}
	wantErr(t, svc.Delete(bg, appActor(user), photo.ID), KindNotFound, "Фотография не найдена")

	store.photos[photo.ID] = photo
	store.failDeletePhoto = errors.New("db down")
	files.deleteLog = nil
	if err := svc.Delete(bg, appActor(user), photo.ID); err == nil || len(files.deleteLog) != 0 {
		t.Fatalf("got %v, deleted %v", err, files.deleteLog)
	}
}
