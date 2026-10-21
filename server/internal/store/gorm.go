package store

import (
	"context"
	"database/sql"

	sqlitegorm "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newStore(db *sql.DB) (*Store, error) {
	orm, err := gorm.Open(sqlitegorm.New(sqlitegorm.Config{Conn: db}), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	return &Store{DB: db, orm: orm}, nil
}

func (s *Store) WithImmediate(ctx context.Context, fn func(*gorm.DB) error) error {
	return s.withImmediateSQL(ctx, func(conn *sql.Conn) error {
		// Bind to the acquired connection; never reenter the single-connection pool.
		// GORM v1.31.2 clones Statement when Session.Context is set; the isolation test guards upgrades.
		db := s.orm.Session(&gorm.Session{NewDB: true, Context: ctx})
		db.Statement.ConnPool = conn
		return fn(db)
	})
}

// These rows keep the existing integer timestamps and database defaults explicit.
type userRow struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    int64 `gorm:"autoCreateTime:false"`
	UpdatedAt    int64 `gorm:"autoUpdateTime:false"`
}

type foodRow struct {
	ID                 int64
	Name               string
	Status             string
	EnergyKcal100g     *float64 `gorm:"column:energy_kcal_100g"`
	ProteinG100g       *float64 `gorm:"column:protein_g_100g"`
	FatG100g           *float64 `gorm:"column:fat_g_100g"`
	CarbohydrateG100g  *float64 `gorm:"column:carbohydrate_g_100g"`
	NutritionSource    *string
	NutritionReference *string
	DensityGml         *float64 `gorm:"column:density_g_ml"`
	PieceWeightG       *float64
	ConversionSource   *string
	AllergensJSON      string `gorm:"column:allergens_json"`
	AllergenComplete   bool
	AllergenSource     *string
	Version            int64
	CreatedAt          int64 `gorm:"autoCreateTime:false"`
	UpdatedAt          int64 `gorm:"autoUpdateTime:false"`
}

type inventoryRow struct {
	ID            int64
	UserID        int64
	FoodID        int64
	QuantityMilli int64
	Unit          string
	ExpiresOn     *string
	Note          string
	Version       int64
	CreatedAt     int64 `gorm:"autoCreateTime:false"`
	UpdatedAt     int64 `gorm:"autoUpdateTime:false"`
}
