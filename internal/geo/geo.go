package geo

type Country struct {
	Code   string `gorm:"primaryKey;size:2"`
	NameEn string `gorm:"size:128;not null"`
	NameRu string `gorm:"size:128;not null;index"`

	Continent string `gorm:"size:2"`
	Cities    []City `gorm:"foreignKey:CountryCode;references:Code"`
}

type City struct {
	ID          uint    `gorm:"primaryKey;autoIncrement:false"`
	CountryCode string  `gorm:"size:2;not null;index"`
	NameEn      string  `gorm:"size:200;not null;index"`
	NameRu      string  `gorm:"size:200;index"`
	Latitude    float64 `gorm:"index"`
	Longitude   float64 `gorm:"index"`

	Population int     `gorm:"index"`
	Country    Country `gorm:"foreignKey:CountryCode;references:Code"`
}

func (c City) Name() string {
	if c.NameRu != "" {
		return c.NameRu
	}
	return c.NameEn
}

type CityFilter struct {
	CountryCode string
	Query       string

	AfterID uint
	Limit   int
}
