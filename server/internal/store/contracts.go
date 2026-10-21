package store

import (
	"database/sql"
	"time"

	"gorm.io/gorm"
)

type Store struct {
	DB  *sql.DB
	orm *gorm.DB
}
type AppError struct {
	Status        int
	Code, Message string
}

func (e *AppError) Error() string              { return e.Message }
func E(status int, code, message string) error { return &AppError{status, code, message} }

type User struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	Role               string    `json:"role"`
	Status             string    `json:"status"`
	MustChangePassword bool      `json:"must_change_password"`
	AuthVersion        int64     `json:"-"`
	PasswordHash       string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
type Session struct {
	User      User
	TokenHash []byte
	ExpiresAt time.Time
}
type Food struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
type Preferences struct {
	Allergens          []string  `json:"allergens"`
	ExcludedFoodIDs    []int64   `json:"excluded_food_ids"`
	DietaryPreferences []string  `json:"dietary_preferences"`
	DailyEnergyKcal    *float64  `json:"daily_energy_kcal"`
	DailyProteinG      *float64  `json:"daily_protein_g"`
	DailyFatG          *float64  `json:"daily_fat_g"`
	DailyCarbohydrateG *float64  `json:"daily_carbohydrate_g"`
	ExcludedFoods      []Food    `json:"excluded_foods"`
	UpdatedAt          time.Time `json:"updated_at"`
}
type Audit struct {
	ID           int64             `json:"id"`
	ActorUserID  *int64            `json:"actor_user_id"`
	ActorSource  string            `json:"actor_source"`
	Action       string            `json:"action"`
	TargetUserID int64             `json:"target_user_id"`
	Details      map[string]string `json:"details"`
	CreatedAt    time.Time         `json:"created_at"`
}
