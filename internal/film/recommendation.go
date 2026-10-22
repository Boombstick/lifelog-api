package film

type FilmRecommendation struct {
	FilmID    uint `gorm:"primaryKey"`
	SimilarID uint `gorm:"primaryKey;index"`
	Position  int  `gorm:"not null"`
}

func (FilmRecommendation) TableName() string { return "film_recommendations" }
