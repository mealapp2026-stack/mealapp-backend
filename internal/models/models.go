package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MealSlot string

const (
	MealSlotBreakfast MealSlot = "breakfast"
	MealSlotLunch     MealSlot = "lunch"
	MealSlotDinner    MealSlot = "dinner"
)

type Cuisine string

const (
	CuisineAfrican       Cuisine = "african"
	CuisineFrench        Cuisine = "french"
	CuisineChinese       Cuisine = "chinese"
	CuisineMediterranean Cuisine = "mediterranean"
	CuisineIndian        Cuisine = "indian"
)

type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name         string             `bson:"name" json:"name"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"passwordHash" json:"-"`
	Profile      *HealthProfile     `bson:"profile,omitempty" json:"profile,omitempty"`
	Preferences  MealPreferences    `bson:"preferences" json:"preferences"`
	CreatedAt    time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type HealthProfile struct {
	Goal          string   `bson:"goal" json:"goal"`
	Diet          string   `bson:"diet" json:"diet"`
	Activity      string   `bson:"activity" json:"activity"`
	Allergies     []string `bson:"allergies" json:"allergies"`
	DislikedFoods []string `bson:"dislikedFoods" json:"dislikedFoods"`
}

type MealPreferences struct {
	Breakfast Cuisine `bson:"breakfast" json:"breakfast"`
	Lunch     Cuisine `bson:"lunch" json:"lunch"`
	Dinner    Cuisine `bson:"dinner" json:"dinner"`
}

func DefaultMealPreferences() MealPreferences {
	return MealPreferences{
		Breakfast: CuisineFrench,
		Lunch:     CuisineAfrican,
		Dinner:    CuisineChinese,
	}
}

type Meal struct {
	Slot        MealSlot `json:"slot"`
	Cuisine     Cuisine  `json:"cuisine"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Calories    int      `json:"calories"`
	Protein     string   `json:"protein"`
	PrepTime    string   `json:"prepTime"`
	Tags        []string `json:"tags"`
}

type MealLibraryItem struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Meal        Meal               `bson:"meal" json:"meal"`
	Ingredients []string           `bson:"ingredients" json:"ingredients"`
	Diets       []string           `bson:"diets" json:"diets"`
	Allergens   []string           `bson:"allergens" json:"allergens"`
	Source      string             `bson:"source" json:"source"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

type DailyMealPlan struct {
	Date  string `json:"date"`
	Meals []Meal `json:"meals"`
}
