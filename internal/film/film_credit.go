package film

type CreditRole string

const (
	CreditRoleDirector CreditRole = "director"
	CreditRoleActor    CreditRole = "actor"

	CreditRoleCreator CreditRole = "creator"
)

type FilmCredit struct {
	FilmID       uint       `gorm:"primaryKey"`
	PersonID     uint       `gorm:"primaryKey"`
	Role         CreditRole `gorm:"primaryKey;size:16"`
	BillingOrder int
	Film         Film   `gorm:"foreignKey:FilmID"`
	Person       Person `gorm:"foreignKey:PersonID"`
}
