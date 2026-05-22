package store

import (
	"context"
	"errors"
	"time"

	"mealapp-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MealStore struct {
	collection *mongo.Collection
}

func NewMealStore(db *mongo.Database) *MealStore {
	return &MealStore{collection: db.Collection("meals")}
}

func (s *MealStore) EnsureIndexes(ctx context.Context) error {
	_, err := s.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "meal.slot", Value: 1},
				{Key: "meal.cuisine", Value: 1},
			},
		},
		{
			Keys: bson.D{{Key: "meal.name", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetCollation(&options.Collation{Locale: "en", Strength: 2}),
		},
	})
	return err
}

func (s *MealStore) SeedDefaults(ctx context.Context) error {
	now := time.Now().UTC()
	for _, meal := range DefaultMealLibraryItems() {
		meal.CreatedAt = now
		meal.UpdatedAt = now

		_, err := s.collection.UpdateOne(
			ctx,
			bson.M{"meal.name": meal.Meal.Name},
			bson.M{
				"$set": bson.M{
					"meal":        meal.Meal,
					"ingredients": meal.Ingredients,
					"diets":       meal.Diets,
					"allergens":   meal.Allergens,
					"source":      meal.Source,
					"updatedAt":   now,
				},
				"$setOnInsert": bson.M{
					"createdAt": now,
				},
			},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *MealStore) FindBySlot(ctx context.Context, slot models.MealSlot) ([]models.MealLibraryItem, error) {
	cursor, err := s.collection.Find(ctx, bson.M{"meal.slot": slot})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var meals []models.MealLibraryItem
	if err := cursor.All(ctx, &meals); err != nil {
		return nil, err
	}
	return meals, nil
}

func (s *MealStore) InsertGenerated(ctx context.Context, item models.MealLibraryItem) error {
	if item.Meal.Name == "" {
		return errors.New("generated meal name is required")
	}
	now := time.Now().UTC()
	item.Source = "ai"
	item.CreatedAt = now
	item.UpdatedAt = now
	_, err := s.collection.InsertOne(ctx, item)
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}
