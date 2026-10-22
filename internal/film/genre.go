package film

type Genre struct {
	ID     string `gorm:"primarykey"`
	TMDBID int    `gorm:"uniqueIndex;not null"`
	Films  []Film `gorm:"many2many:film_genres;"`
	NameRu string `gorm:"size:64;not null"`
	NameEn string `gorm:"size:64;not null"`
}
