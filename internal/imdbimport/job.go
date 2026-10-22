package imdbimport

import "time"

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

const (
	SourceIMDb = "imdb"

	InterruptedMessage = "прервано перезапуском сервиса — запустите импорт снова"

	MaxNotFoundItems = 100
)

type NotFoundItem struct {
	IMDbID string `json:"imdb_id"`
	Title  string `json:"title"`
	Year   *int   `json:"year"`
}

type Job struct {
	ID              uint
	UserID          uint
	Source          string
	Kind            ListKind
	Status          Status
	Total           int
	Processed       int
	Added           int
	SkippedExisting int
	NotFound        int
	Unsupported     int
	Failed          int
	CatalogAdded    int
	Error           *string
	NotFoundItems   []NotFoundItem
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

type outcome int

const (
	outcomeAdded outcome = iota
	outcomeSkipped
	outcomeNotFound
	outcomeUnsupported
	outcomeFailed
)

func (j *Job) record(row Row, result outcome, catalogAdded bool) {
	j.Processed++
	if catalogAdded {
		j.CatalogAdded++
	}
	switch result {
	case outcomeAdded:
		j.Added++
	case outcomeSkipped:
		j.SkippedExisting++
	case outcomeUnsupported:
		j.Unsupported++
	case outcomeFailed:
		j.Failed++
	case outcomeNotFound:
		j.NotFound++
		if len(j.NotFoundItems) < MaxNotFoundItems {
			item := NotFoundItem{IMDbID: row.IMDbID, Title: row.Title}
			if row.Year > 0 {
				year := row.Year
				item.Year = &year
			}
			j.NotFoundItems = append(j.NotFoundItems, item)
		}
	}
}

type JobResponse struct {
	ID              uint     `json:"id"`
	Source          string   `json:"source"`
	Kind            ListKind `json:"kind"`
	Status          Status   `json:"status"`
	Total           int      `json:"total"`
	Processed       int      `json:"processed"`
	Added           int      `json:"added"`
	SkippedExisting int      `json:"skipped_existing"`
	NotFound        int      `json:"not_found"`
	Unsupported     int      `json:"unsupported"`
	Failed          int      `json:"failed"`
	CatalogAdded    int      `json:"catalog_added"`
	Error           *string  `json:"error"`

	NotFoundItems *[]NotFoundItem `json:"not_found_items,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

type JobsResponse struct {
	Items []JobResponse `json:"items"`
}

func (j Job) response(withItems bool) JobResponse {
	resp := JobResponse{
		ID:              j.ID,
		Source:          j.Source,
		Kind:            j.Kind,
		Status:          j.Status,
		Total:           j.Total,
		Processed:       j.Processed,
		Added:           j.Added,
		SkippedExisting: j.SkippedExisting,
		NotFound:        j.NotFound,
		Unsupported:     j.Unsupported,
		Failed:          j.Failed,
		CatalogAdded:    j.CatalogAdded,
		Error:           j.Error,
		CreatedAt:       j.CreatedAt.UTC(),
	}
	if j.FinishedAt != nil {
		finished := j.FinishedAt.UTC()
		resp.FinishedAt = &finished
	}
	if withItems {
		items := j.NotFoundItems
		if items == nil {
			items = []NotFoundItem{}
		}
		resp.NotFoundItems = &items
	}
	return resp
}
