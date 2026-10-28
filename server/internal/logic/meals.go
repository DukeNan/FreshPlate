package logic

import (
	"context"
	"freshplate/server/internal/store"
)

func (s *Service) CreateMeal(ctx context.Context, actor store.Session, input store.MealInput) (store.Meal, error) {
	return s.Store.CreateMeal(ctx, actor, input, s.Now(), func(p store.Preferences, recipe store.RecipeDetail) error {
		if recipe.HasDisabledFoods || len(recipe.Ingredients) == 0 {
			return store.E(400, "DIETARY_RESTRICTION", "食谱食材不可用")
		}
		ids := make([]int64, 0, len(recipe.Ingredients))
		for _, item := range recipe.Ingredients {
			ids = append(ids, item.FoodID)
		}
		return EnforceRestrictions(p, ids, recipe.EffectiveAllergens, recipe.RestrictionMetadataComplete)
	})
}
