package film

type Collection struct {
	TMDBID     int    `gorm:"primaryKey;autoIncrement:false"`
	Name       string `gorm:"size:255;not null"`
	PosterURL  string `gorm:"size:512"`
	PosterPath string `gorm:"size:255"`
}

func (Collection) TableName() string { return "collections" }
