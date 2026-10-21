package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	migfs "freshplate/server/migrations"
	"gorm.io/gorm"
)

func TestGORMOpensExistingV5WithoutSchemaChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db := legacyDB(t, path)
	legacyExec(t, db, "PRAGMA foreign_keys=ON")
	legacyExec(t, db, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, completed_at INTEGER NOT NULL)")
	for _, m := range migrationChain {
		script, err := migfs.Files.ReadFile(m.file)
		if err != nil {
			t.Fatal(err)
		}
		legacyExec(t, db, string(script))
		legacyExec(t, db, "INSERT INTO schema_migrations VALUES(?,?,123)", m.version, embeddedChecksum(t, m.file))
	}
	hash := bytes.Repeat([]byte{42}, 32)
	legacyExec(t, db, "INSERT INTO users(id,username,password_hash,role,auth_version,created_at,updated_at) VALUES(41,'legacy','legacy-hash','user',7,0,222)")
	legacyExec(t, db, "INSERT INTO user_preferences(user_id,updated_at) VALUES(41,0)")
	legacyExec(t, db, "INSERT INTO sessions VALUES(?,41,7,0,?)", hash, testNow.Add(time.Hour).Unix())
	legacyExec(t, db, "INSERT INTO refresh_tokens VALUES(?,?,0)", hash, hash)
	legacyExec(t, db, "INSERT INTO inventory_items(id,user_id,food_id,quantity_milli,unit,note,version,created_at,updated_at) VALUES(73,41,1,0,'g','existing',4,0,222)")
	legacyExec(t, db, "INSERT INTO recipes(id,visibility,owner_user_id,status,name,servings,duration_minutes,steps_json,allergen_complete,version,created_at,updated_at) VALUES(81,'private',41,'draft','existing recipe',1,1,'[]',0,3,0,222)")
	legacyExec(t, db, "INSERT INTO recipe_ingredients VALUES(81,1,1,2000,'g')")
	var before string
	schema := "SELECT group_concat(sql, char(10)) FROM (SELECT sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type,name)"
	if err := db.QueryRow(schema).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.UserByName(ctx, "legacy")
	if err != nil || u.ID != 41 || u.AuthVersion != 7 || u.PasswordHash != "legacy-hash" || u.CreatedAt.Unix() != 0 || u.UpdatedAt.Unix() != 222 {
		t.Fatalf("legacy user: %+v %v", u, err)
	}
	ss, err := s.ResolveRefresh(ctx, hash, testNow)
	if err != nil || ss == nil {
		t.Fatalf("legacy session: %+v %v", ss, err)
	}
	item, err := s.InventoryByID(ctx, *ss, 73, testNow)
	if err != nil || item.QuantityMilli != 0 || item.Version != 4 || item.ExpiresOn != nil || item.CreatedAt.Unix() != 0 {
		t.Fatalf("legacy inventory: %+v %v", item, err)
	}
	recipe, err := s.RecipeDetail(ctx, 81, RecipeDetailReadScope{ViewerID: 41})
	if err != nil || recipe.Version != 3 || len(recipe.Ingredients) != 1 || recipe.Ingredients[0].QuantityMilli != 2000 || recipe.CreatedAt.Unix() != 0 {
		t.Fatalf("legacy recipe: %+v %v", recipe, err)
	}
	p, err := s.Preferences(ctx, 41)
	if err != nil || p.DailyEnergyKcal != nil || p.UpdatedAt.Unix() != 0 {
		t.Fatalf("legacy preferences: %+v %v", p, err)
	}
	if _, err = s.RotateRefresh(ctx, hash, bytes.Repeat([]byte{43}, 32), testNow); err != nil {
		t.Fatal(err)
	}
	var after string
	if err = s.DB.QueryRow(schema).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || queryInt(t, s.DB, "SELECT count(*) FROM schema_migrations WHERE completed_at=123") != len(migrationChain) {
		t.Fatal("opening or using existing database changed schema/history")
	}
}

func TestGORMZeroValuesAndNullUpdates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	actor := adminSession(t, s)
	epoch := time.Unix(0, 0).UTC()
	u, err := s.CreateUser(ctx, "epoch", "hash", "user", epoch)
	if err != nil || !u.CreatedAt.Equal(epoch) || !u.UpdatedAt.Equal(epoch) {
		t.Fatalf("explicit user timestamps: %+v %v", u, err)
	}
	foodCreatedAt := epoch.Add(time.Second)
	food, err := s.CreateFood(ctx, actor, fullFood("zero test"), foodCreatedAt)
	if err != nil || !food.CreatedAt.Equal(foodCreatedAt) || !food.UpdatedAt.Equal(foodCreatedAt) {
		t.Fatalf("explicit food timestamps: %+v %v", food, err)
	}
	input := fullFood(food.Name)
	input.EnergyKcal100g, input.ProteinG100g, input.FatG100g, input.CarbohydrateG100g = f64ptr(0), f64ptr(0), f64ptr(0), f64ptr(0)
	food, err = s.UpdateFood(ctx, actor, food.ID, food.Version, input, testNow)
	if err != nil || food.EnergyKcal100g == nil || *food.EnergyKcal100g != 0 || food.ProteinG100g == nil || *food.ProteinG100g != 0 {
		t.Fatalf("zero nutrition: %+v %v", food, err)
	}
	foodID := food.ID
	food, err = s.UpdateFood(ctx, actor, food.ID, food.Version, plainFood(food.Name), epoch)
	if err != nil || food.AllergenComplete || len(food.Allergens) != 0 || food.AllergenSource != nil || food.EnergyKcal100g != nil || food.DensityGml != nil {
		t.Fatalf("cleared food: %+v %v", food, err)
	}
	if food.ID != foodID || food.Version != 3 || !food.CreatedAt.Equal(foodCreatedAt) || !food.UpdatedAt.Equal(epoch) {
		t.Fatalf("food update identity/version/timestamps: %+v", food)
	}
	item, err := s.CreateInventory(ctx, actor, food.ID, 1000, "g", strptr("2026-11-01"), "note", epoch)
	if err != nil || !item.CreatedAt.Equal(epoch) || !item.UpdatedAt.Equal(epoch) {
		t.Fatalf("explicit inventory timestamps: %+v %v", item, err)
	}
	item, err = s.UpdateInventory(ctx, actor, item.ID, item.Version, 0, nil, "", testNow)
	if err != nil || item.QuantityMilli != 0 || item.ExpiresOn != nil || item.Note != "" {
		t.Fatalf("cleared inventory: %+v %v", item, err)
	}
	recipe, err := s.CreateRecipe(ctx, actor, true, validRecipeWrite("epoch recipe", food.ID), epoch)
	if err != nil || !recipe.CreatedAt.Equal(epoch) || !recipe.UpdatedAt.Equal(epoch) {
		t.Fatalf("explicit recipe timestamps: %+v %v", recipe, err)
	}
	recipeInput := validRecipeWrite(recipe.Name, food.ID)
	recipeInput.AllergenComplete, recipeInput.AllergenSource, recipeInput.Source = false, nil, ""
	recipeInput.Steps, recipeInput.AdditionalAllergens, recipeInput.Ingredients = []string{}, []string{}, []RecipeIngredientInput{}
	recipe, err = s.UpdateRecipe(ctx, actor, true, recipe.ID, recipe.Version, "draft", recipeInput, testNow)
	if err != nil || recipe.AllergenComplete || recipe.AllergenSource != nil || recipe.Source != "" || len(recipe.Steps) != 0 || len(recipe.Ingredients) != 0 || len(recipe.AdditionalAllergens) != 0 {
		t.Fatalf("cleared recipe: %+v %v", recipe, err)
	}
	p, err := s.Preferences(ctx, actor.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.DailyEnergyKcal = f64ptr(2000)
	if _, err = s.SavePreferences(ctx, actor, p, testNow); err != nil {
		t.Fatal(err)
	}
	p.DailyEnergyKcal = nil
	p, err = s.SavePreferences(ctx, actor, p, epoch)
	if err != nil || p.DailyEnergyKcal != nil || !p.UpdatedAt.Equal(epoch) {
		t.Fatalf("cleared preferences: %+v %v", p, err)
	}
}

func TestGORMNotFoundCompatibility(t *testing.T) {
	s := testStore(t)
	if _, err := s.UserByName(context.Background(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user: %v", err)
	}
	if err := mapError(fmt.Errorf("lookup: %w", gorm.ErrRecordNotFound)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GORM not found: %v", err)
	}
}

func TestGORMWithImmediateConnectionIsolation(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			s := testStore(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.orm.WithContext(ctx).Exec("CREATE TABLE isolation_probe (value INTEGER)").Error; err != nil {
				t.Fatal(err)
			}
			statement, pool := s.orm.Statement, s.orm.Statement.ConnPool
			abort := errors.New("rollback probe")
			err := s.WithImmediate(ctx, func(tx *gorm.DB) error {
				if tx.Statement == statement || tx.Statement.ConnPool == pool {
					t.Fatal("transaction did not get an isolated statement and connection")
				}
				if s.orm.Statement != statement || s.orm.Statement.ConnPool != pool {
					t.Fatal("transaction changed the shared ORM statement or connection pool")
				}
				if err := tx.Exec("INSERT INTO isolation_probe VALUES (1)").Error; err != nil {
					return err
				}
				if rollback {
					return abort
				}
				return nil
			})
			if (rollback && !errors.Is(err, abort)) || (!rollback && err != nil) {
				t.Fatalf("transaction: %v", err)
			}
			var count int64
			if err := s.orm.WithContext(ctx).Table("isolation_probe").Count(&count).Error; err != nil {
				t.Fatalf("shared ORM after transaction: %v", err)
			}
			want := int64(1)
			if rollback {
				want = 0
			}
			if count != want {
				t.Fatalf("persisted rows = %d, want %d", count, want)
			}
		})
	}
}
