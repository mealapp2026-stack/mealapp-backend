package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"mealapp-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrNotFound = errors.New("not found")

type UserStore struct {
	collection *mongo.Collection
}

func NewUserStore(db *mongo.Database) *UserStore {
	return &UserStore{collection: db.Collection("users")}
}

func (s *UserStore) EnsureIndexes(ctx context.Context) error {
	_, err := s.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func (s *UserStore) Create(ctx context.Context, user models.User) (models.User, error) {
	now := time.Now().UTC()
	user.ID = primitive.NewObjectID()
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	user.Preferences = models.DefaultMealPreferences()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := s.collection.InsertOne(ctx, user)
	if err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (s *UserStore) FindByEmail(ctx context.Context, email string) (models.User, error) {
	var user models.User
	err := s.collection.FindOne(ctx, bson.M{"email": strings.ToLower(strings.TrimSpace(email))}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.User{}, ErrNotFound
	}
	return user, err
}

func (s *UserStore) FindByID(ctx context.Context, id primitive.ObjectID) (models.User, error) {
	var user models.User
	err := s.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.User{}, ErrNotFound
	}
	return user, err
}

func (s *UserStore) UpdateProfile(ctx context.Context, id primitive.ObjectID, profile models.HealthProfile) (models.User, error) {
	update := bson.M{
		"$set": bson.M{
			"profile":   profile,
			"updatedAt": time.Now().UTC(),
		},
	}
	return s.findOneAndUpdate(ctx, id, update)
}

func (s *UserStore) UpdatePreferences(ctx context.Context, id primitive.ObjectID, preferences models.MealPreferences) (models.User, error) {
	update := bson.M{
		"$set": bson.M{
			"preferences": preferences,
			"updatedAt":   time.Now().UTC(),
		},
	}
	return s.findOneAndUpdate(ctx, id, update)
}

func (s *UserStore) findOneAndUpdate(ctx context.Context, id primitive.ObjectID, update bson.M) (models.User, error) {
	var user models.User
	err := s.collection.FindOneAndUpdate(
		ctx,
		bson.M{"_id": id},
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return models.User{}, ErrNotFound
	}
	return user, err
}
