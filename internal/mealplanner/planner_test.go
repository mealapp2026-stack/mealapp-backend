package mealplanner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mealapp-backend/internal/models"
	"mealapp-backend/internal/store"
)

func TestPlannerAvoidsAllergies(t *testing.T) {
	planner := New(&memoryLibrary{items: store.DefaultMealLibraryItems()}, nil)
	plan, err := planner.ForDate(
		context.Background(),
		time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		models.MealPreferences{
			Breakfast: models.CuisineIndian,
			Lunch:     models.CuisineAfrican,
			Dinner:    models.CuisineMediterranean,
		},
		&models.HealthProfile{
			Diet:      "No restriction",
			Allergies: []string{"peanuts", "fish"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, meal := range plan.Meals {
		search := strings.ToLower(meal.Name + " " + meal.Description + " " + strings.Join(meal.Tags, " "))
		if strings.Contains(search, "peanut") || strings.Contains(search, "fish") || strings.Contains(search, "salmon") {
			t.Fatalf("meal should avoid user allergies: %+v", meal)
		}
	}
}

func TestPlannerRespectsVegetarianDiet(t *testing.T) {
	planner := New(&memoryLibrary{items: store.DefaultMealLibraryItems()}, nil)
	plan, err := planner.ForDate(
		context.Background(),
		time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		models.MealPreferences{
			Breakfast: models.CuisineFrench,
			Lunch:     models.CuisineChinese,
			Dinner:    models.CuisineAfrican,
		},
		&models.HealthProfile{
			Diet: "Vegetarian",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	blocked := []string{"chicken", "fish", "salmon", "turkey"}
	for _, meal := range plan.Meals {
		search := strings.ToLower(meal.Name + " " + meal.Description)
		for _, word := range blocked {
			if strings.Contains(search, word) {
				t.Fatalf("vegetarian meal should not contain %q: %+v", word, meal)
			}
		}
	}
}

func TestPlannerHonorsCuisineWhenSafe(t *testing.T) {
	planner := New(&memoryLibrary{items: store.DefaultMealLibraryItems()}, nil)
	plan, err := planner.ForDate(
		context.Background(),
		time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		models.MealPreferences{
			Breakfast: models.CuisineFrench,
			Lunch:     models.CuisineAfrican,
			Dinner:    models.CuisineChinese,
		},
		&models.HealthProfile{
			Diet: "No restriction",
			Goal: "Build muscle",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[models.MealSlot]models.Cuisine{
		models.MealSlotBreakfast: models.CuisineFrench,
		models.MealSlotLunch:     models.CuisineAfrican,
		models.MealSlotDinner:    models.CuisineChinese,
	}

	for _, meal := range plan.Meals {
		if meal.Cuisine != expected[meal.Slot] {
			t.Fatalf("expected %s %s, got %+v", meal.Slot, expected[meal.Slot], meal)
		}
	}
}

func TestPlannerUsesAIWhenLibraryHasNoSafeMeal(t *testing.T) {
	library := memoryLibrary{items: nil}
	ai := fakeAI{
		item: models.MealLibraryItem{
			Meal: models.Meal{
				Slot:        models.MealSlotDinner,
				Cuisine:     models.CuisineIndian,
				Name:        "AI palak millet plate",
				Description: "A safe generated dinner.",
				Calories:    500,
				Protein:     "30g",
				PrepTime:    "25 min",
				Tags:        []string{"Generated"},
			},
		},
	}
	planner := New(&library, ai)

	plan, err := planner.ForDate(
		context.Background(),
		time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		models.MealPreferences{
			Breakfast: models.CuisineIndian,
			Lunch:     models.CuisineIndian,
			Dinner:    models.CuisineIndian,
		},
		&models.HealthProfile{Diet: "Vegetarian"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Meals) != 3 {
		t.Fatalf("expected 3 meals, got %d", len(plan.Meals))
	}
	if library.generated == 0 {
		t.Fatal("expected generated AI meals to be stored in the library")
	}
}

func TestPlannerAlternativeExcludesCurrentMeal(t *testing.T) {
	planner := New(&memoryLibrary{items: store.DefaultMealLibraryItems()}, nil)

	meal, err := planner.Alternative(
		context.Background(),
		models.MealSlotBreakfast,
		models.CuisineIndian,
		&models.HealthProfile{Diet: "No restriction"},
		[]string{"Vegetable poha with boiled egg"},
		false,
		"test-request",
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if meal.Name == "Vegetable poha with boiled egg" {
		t.Fatalf("expected another Indian breakfast, got same meal: %+v", meal)
	}
	if meal.Slot != models.MealSlotBreakfast {
		t.Fatalf("expected breakfast alternative, got %+v", meal)
	}
}

func TestPlannerAlternativeCanForceAI(t *testing.T) {
	library := memoryLibrary{items: store.DefaultMealLibraryItems()}
	ai := fakeAI{
		item: models.MealLibraryItem{
			Meal: models.Meal{
				Name:        "AI masala oats bowl",
				Description: "A generated breakfast.",
				Calories:    410,
				Protein:     "24g",
				PrepTime:    "15 min",
				Tags:        []string{"Generated"},
			},
		},
	}
	planner := New(&library, ai)

	meal, err := planner.Alternative(
		context.Background(),
		models.MealSlotBreakfast,
		models.CuisineIndian,
		&models.HealthProfile{Diet: "No restriction"},
		[]string{"Vegetable poha with boiled egg", "Moong dal chilla with mint chutney"},
		true,
		"test-request",
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if meal.Name != "AI masala oats bowl" {
		t.Fatalf("expected forced AI meal, got %+v", meal)
	}
}

func TestPlannerAlternativeRejectsRepeatedAIMeal(t *testing.T) {
	library := memoryLibrary{items: nil}
	ai := fakeAI{
		item: models.MealLibraryItem{
			Meal: models.Meal{
				Name:        "Vegetable poha with boiled egg",
				Description: "Repeated meal.",
				Calories:    410,
				Protein:     "24g",
				PrepTime:    "15 min",
				Tags:        []string{"Generated"},
			},
		},
	}
	planner := New(&library, ai)

	_, err := planner.Alternative(
		context.Background(),
		models.MealSlotBreakfast,
		models.CuisineIndian,
		&models.HealthProfile{Diet: "No restriction"},
		[]string{"Vegetable poha with boiled egg"},
		true,
		"test-request",
		1,
	)
	if !errors.Is(err, ErrAIRepeatedMeal) {
		t.Fatalf("expected repeated meal error, got %v", err)
	}
}

type memoryLibrary struct {
	items     []models.MealLibraryItem
	generated int
}

func (l memoryLibrary) FindBySlot(_ context.Context, slot models.MealSlot) ([]models.MealLibraryItem, error) {
	var meals []models.MealLibraryItem
	for _, item := range l.items {
		if item.Meal.Slot == slot {
			meals = append(meals, item)
		}
	}
	return meals, nil
}

func (l *memoryLibrary) InsertGenerated(_ context.Context, item models.MealLibraryItem) error {
	l.generated++
	l.items = append(l.items, item)
	return nil
}

type fakeAI struct {
	item models.MealLibraryItem
}

func (f fakeAI) GenerateMeal(_ context.Context, request AIRequest) (models.MealLibraryItem, error) {
	item := f.item
	item.Meal.Slot = request.Slot
	item.Meal.Cuisine = request.PreferredCuisine
	return item, nil
}
