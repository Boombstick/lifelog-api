package tmdb

import (
	"strings"
	"time"
)

type Movie struct {
	ID            int     `json:"id"`
	IMDBID        string  `json:"imdb_id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	Popularity    float64 `json:"popularity"`
	Runtime       int     `json:"runtime"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	ReleaseDate   string  `json:"release_date"`
	Adult         bool    `json:"adult"`

	BelongsToCollection *MovieCollection `json:"belongs_to_collection"`

	Genres       []Genre      `json:"genres"`
	Credits      Credits      `json:"credits"`
	Translations Translations `json:"translations"`
}

type MovieCollection struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	PosterPath string `json:"poster_path"`
}

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type genreListResponse struct {
	Genres []Genre `json:"genres"`
}

type Credits struct {
	Cast []CastMember `json:"cast"`
	Crew []CrewMember `json:"crew"`
}

type CastMember struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Order        int    `json:"order"`
}

type CrewMember struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Job          string `json:"job"`
}

type Translations struct {
	Translations []Translation `json:"translations"`
}

type Translation struct {
	ISO639_1  string          `json:"iso_639_1"`
	ISO3166_1 string          `json:"iso_3166_1"`
	Data      TranslationData `json:"data"`
}

type TranslationData struct {
	Title string `json:"title"`

	Name     string `json:"name"`
	Overview string `json:"overview"`
}

func (t Translations) Title(lang string) (string, bool) {
	for _, tr := range t.Translations {
		if tr.ISO639_1 != lang {
			continue
		}
		if tr.Data.Title != "" {
			return tr.Data.Title, true
		}
		if tr.Data.Name != "" {
			return tr.Data.Name, true
		}
	}
	return "", false
}

func (t Translations) Overview(lang string) (string, bool) {
	for _, tr := range t.Translations {
		if tr.ISO639_1 == lang && tr.Data.Overview != "" {
			return tr.Data.Overview, true
		}
	}
	return "", false
}

func (c Credits) Directors() []CrewMember {
	var out []CrewMember
	for _, member := range c.Crew {
		if member.Job == "Director" {
			out = append(out, member)
		}
	}
	return out
}

type discoverResponse struct {
	Page         int          `json:"page"`
	TotalPages   int          `json:"total_pages"`
	TotalResults int          `json:"total_results"`
	Results      []MovieShort `json:"results"`
}

type MovieShort struct {
	ID         int     `json:"id"`
	Title      string  `json:"title"`
	Popularity float64 `json:"popularity"`
}

type Person struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	AlsoKnownAs []string `json:"also_known_as"`
	Department  string   `json:"known_for_department"`
	ProfilePath string   `json:"profile_path"`
}

type AlternativeTitles struct {
	Titles []AlternativeTitle `json:"titles"`
}

type AlternativeTitle struct {
	CountryCode string `json:"iso_3166_1"`
	Title       string `json:"title"`
	Type        string `json:"type"`
}

type Date struct {
	time.Time
}

func (d *Date) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), `"`)
	if value == "" || value == "null" {
		d.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		d.Time = time.Time{}
		return nil
	}
	d.Time = parsed
	return nil
}

func (d Date) Ptr() *time.Time {
	if d.IsZero() {
		return nil
	}
	t := d.Time
	return &t
}

type TVSeries struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	Overview         string  `json:"overview"`
	PosterPath       string  `json:"poster_path"`
	FirstAirDate     Date    `json:"first_air_date"`
	LastAirDate      Date    `json:"last_air_date"`
	EpisodeRunTime   []int   `json:"episode_run_time"`
	Popularity       float64 `json:"popularity"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Status           string  `json:"status"`
	Type             string  `json:"type"`
	InProduction     bool    `json:"in_production"`
	NumberOfSeasons  int     `json:"number_of_seasons"`
	NumberOfEpisodes int     `json:"number_of_episodes"`
	Adult            bool    `json:"adult"`

	NextEpisodeToAir *TVEpisodeShort `json:"next_episode_to_air"`

	Genres            []Genre             `json:"genres"`
	Networks          []TVNetwork         `json:"networks"`
	CreatedBy         []TVCreator         `json:"created_by"`
	Seasons           []TVSeason          `json:"seasons"`
	AggregateCredits  AggregateCredits    `json:"aggregate_credits"`
	ExternalIDs       ExternalIDs         `json:"external_ids"`
	AlternativeTitles TVAlternativeTitles `json:"alternative_titles"`
	Translations      Translations        `json:"translations"`
}

type TVEpisodeShort struct {
	AirDate Date `json:"air_date"`
}

type TVNetwork struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type TVCreator struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
}

type TVSeason struct {
	ID           int    `json:"id"`
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	AirDate      Date   `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
	PosterPath   string `json:"poster_path"`
}

type AggregateCredits struct {
	Cast []AggregateCastMember `json:"cast"`
}

type AggregateCastMember struct {
	ID                int      `json:"id"`
	Name              string   `json:"name"`
	OriginalName      string   `json:"original_name"`
	TotalEpisodeCount int      `json:"total_episode_count"`
	Order             int      `json:"order"`
	Roles             []TVRole `json:"roles"`
}

type TVRole struct {
	Character    string `json:"character"`
	EpisodeCount int    `json:"episode_count"`
}

type ExternalIDs struct {
	IMDBID string `json:"imdb_id"`
}

type TVAlternativeTitles struct {
	Titles []AlternativeTitle `json:"results"`
}
