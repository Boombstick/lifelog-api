package trips

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type PhotoService struct {
	store Store
	files PhotoStore
	clock Clock
}

func NewPhotoService(store Store, files PhotoStore, clock Clock) *PhotoService {
	return &PhotoService{store: store, files: files, clock: clock}
}

var photoExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/heic": ".heic",
}

func (s *PhotoService) List(ctx context.Context, actor Actor, tripID *uuid.UUID) (PhotosResponse, error) {
	id, err := s.photoTripID(ctx, actor, tripID)
	if err != nil {
		return PhotosResponse{}, err
	}
	photos, err := s.store.TripPhotos(ctx, actor.UserID, id)
	if err != nil {
		return PhotosResponse{}, err
	}
	result := make([]PhotoDTO, 0, len(photos))
	for _, p := range photos {
		result = append(result, PhotoDTO{
			ID:        p.ID,
			TripID:    p.TripID,
			FileName:  p.OriginalFileName,
			Caption:   p.Caption,
			SizeBytes: p.SizeBytes,
			CreatedAt: p.CreatedAt,
		})
	}
	return PhotosResponse{Result: result}, nil
}

func (s *PhotoService) photoTripID(ctx context.Context, actor Actor, tripID *uuid.UUID) (uuid.UUID, error) {
	if tripID != nil && *tripID != uuid.Nil {
		return *tripID, nil
	}
	active, err := s.store.ActiveTrips(ctx, actor.UserID)
	if err != nil {
		return uuid.Nil, err
	}
	if len(active) == 0 {
		return uuid.Nil, Validation(MsgNoActiveTrip)
	}
	return active[0].ID, nil
}

type AddPhotoCommand struct {
	TripID  *uuid.UUID
	Caption *string

	FileName    string
	ContentType string
	Size        int64
	Body        io.Reader
}

func (s *PhotoService) Add(ctx context.Context, actor Actor, cmd AddPhotoCommand) (AddPhotoResponse, error) {
	switch {
	case cmd.Size == 0:
		return AddPhotoResponse{}, Validation("Файл не передан")
	case cmd.Size < 0:
		return AddPhotoResponse{}, Validation("Файл пустой")
	case cmd.Size > MaxPhotoBytes:
		return AddPhotoResponse{}, Validation("Файл больше 15 МБ")
	}
	extension, ok := photoExtensions[strings.ToLower(cmd.ContentType)]
	if !ok {
		return AddPhotoResponse{}, Validation("Поддерживаются только JPEG, PNG, WebP и HEIC")
	}

	tripID, err := s.addTripID(ctx, actor, cmd.TripID)
	if err != nil {
		return AddPhotoResponse{}, err
	}

	fileName := baseFileName(cmd.FileName)
	caption := cmd.Caption
	if caption != nil && *caption == "" {
		caption = nil
	}
	if caption != nil && utf8.RuneCountInString(*caption) > MaxCaptionLen {
		return AddPhotoResponse{}, FieldTooLong("caption", MaxCaptionLen)
	}
	if utf8.RuneCountInString(fileName) > MaxOriginalFileNameLen {
		return AddPhotoResponse{}, FieldTooLong("file", MaxOriginalFileNameLen)
	}

	storedName := strings.ReplaceAll(uuid.NewString(), "-", "") + extension
	if err := s.files.Save(ctx, storedName, cmd.Body); err != nil {
		return AddPhotoResponse{}, err
	}

	photo := &Photo{
		ID:               uuid.New(),
		TripID:           tripID,
		StoredFileName:   storedName,
		OriginalFileName: &fileName,
		ContentType:      cmd.ContentType,
		SizeBytes:        cmd.Size,
		Caption:          caption,
		CreatedAt:        s.clock.Now(),
	}
	if err := s.store.InsertPhoto(ctx, photo); err != nil {
		return AddPhotoResponse{}, err
	}
	return AddPhotoResponse{
		ID:        photo.ID,
		TripID:    photo.TripID,
		FileName:  photo.OriginalFileName,
		Caption:   photo.Caption,
		CreatedAt: photo.CreatedAt,
	}, nil
}

func (s *PhotoService) addTripID(ctx context.Context, actor Actor, tripID *uuid.UUID) (uuid.UUID, error) {
	if tripID == nil || *tripID == uuid.Nil {
		return s.photoTripID(ctx, actor, nil)
	}
	trip, err := s.store.OwnedTrip(ctx, actor.UserID, *tripID)
	if err != nil {
		return uuid.Nil, err
	}
	if trip == nil {
		return uuid.Nil, NotFound(MsgTripNotFound)
	}
	return trip.ID, nil
}

func baseFileName(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

type PhotoDownload struct {
	ContentType string

	FileName string
	Body     io.ReadCloser
}

func (s *PhotoService) Open(ctx context.Context, actor Actor, photoID uuid.UUID) (PhotoDownload, error) {
	photo, err := s.ownedPhoto(ctx, actor, photoID)
	if err != nil {
		return PhotoDownload{}, err
	}
	body, err := s.files.Open(ctx, photo.StoredFileName)
	if errors.Is(err, ErrPhotoFileMissing) {
		return PhotoDownload{}, NotFound(MsgPhotoFileLost)
	}
	if err != nil {
		return PhotoDownload{}, err
	}
	fileName := ""
	if photo.OriginalFileName != nil {
		fileName = *photo.OriginalFileName
	}
	return PhotoDownload{ContentType: photo.ContentType, FileName: fileName, Body: body}, nil
}

func (s *PhotoService) Delete(ctx context.Context, actor Actor, photoID uuid.UUID) error {
	photo, err := s.ownedPhoto(ctx, actor, photoID)
	if err != nil {
		return err
	}
	if err := s.store.DeletePhoto(ctx, photo.ID); err != nil {
		return err
	}
	return s.files.Delete(ctx, photo.StoredFileName)
}

func (s *PhotoService) ownedPhoto(ctx context.Context, actor Actor, photoID uuid.UUID) (*Photo, error) {
	photo, err := s.store.OwnedPhoto(ctx, actor.UserID, photoID)
	if err != nil {
		return nil, err
	}
	if photo == nil {
		return nil, NotFound(MsgPhotoNotFound)
	}
	return photo, nil
}
