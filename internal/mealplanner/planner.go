package mealplanner

import (
	"context"
	"errors"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"

	"mealapp-backend/internal/models"
)

var (
	ErrAINotConfigured = errors.New("ai meal generation is not configured")
	ErrAIEmptyMeal     = errors.New("ai returned an empty meal")
	ErrAIRepeatedMeal  = errors.New("ai repeated an excluded meal")
)

type MealLibrary interface {
	FindBySlot(ctx context.Context, slot models.MealSlot) ([]models.MealLibraryItem, error)
	InsertGenerated(ctx context.Context, item models.MealLibraryItem) error
}

type AIProvider interface {
	GenerateMeal(ctx context.Context, request AIRequest) (models.MealLibraryItem, error)
}

type AIRequest struct {
	Date             string
	RequestID        string
	Attempt          int
	Slot             models.MealSlot
	PreferredCuisine models.Cuisine
	Profile          *models.HealthProfile
	BlockedTerms     []string
}

type Planner struct {
	library MealLibrary
	ai      AIProvider
}

func New(library MealLibrary, ai AIProvider) Planner {
	return Planner{library: library, ai: ai}
}

func (p Planner) Today(ctx context.Context, preferences models.MealPreferences, profile *models.HealthProfile) (models.DailyMealPlan, error) {
	return p.ForDate(ctx, time.Now().UTC(), preferences, profile)
}

func (p Planner) Alternative(ctx context.Context, slot models.MealSlot, preferredCuisine models.Cuisine, profile *models.HealthProfile, excludeNames []string, forceAI bool, requestID string, attempt int) (models.Meal, error) {
	return p.pickAlternative(ctx, time.Now().UTC().Format("2006-01-02"), slot, preferredCuisine, profile, excludeNames, forceAI, requestID, attempt)
}

func (p Planner) ForDate(ctx context.Context, date time.Time, preferences models.MealPreferences, profile *models.HealthProfile) (models.DailyMealPlan, error) {
	day := date.UTC().Format("2006-01-02")
	meals := make([]models.Meal, 0, 3)

	for _, request := range []struct {
		slot    models.MealSlot
		cuisine models.Cuisine
	}{
		{models.MealSlotBreakfast, preferences.Breakfast},
		{models.MealSlotLunch, preferences.Lunch},
		{models.MealSlotDinner, preferences.Dinner},
	} {
		meal, err := p.pick(ctx, day, request.slot, request.cuisine, profile, nil)
		if err != nil {
			return models.DailyMealPlan{}, err
		}
		meals = append(meals, meal)
	}

	return models.DailyMealPlan{Date: day, Meals: meals}, nil
}

func (p Planner) pick(ctx context.Context, day string, slot models.MealSlot, preferredCuisine models.Cuisine, profile *models.HealthProfile, excludeNames []string) (models.Meal, error) {
	libraryMeals, err := p.library.FindBySlot(ctx, slot)
	if err != nil {
		return models.Meal{}, err
	}

	candidates := filterCandidates(libraryMeals, preferredCuisine, profile, true)
	candidates = excludeMeals(candidates, excludeNames)
	if len(candidates) == 0 {
		candidates = filterCandidates(libraryMeals, preferredCuisine, profile, false)
		candidates = excludeMeals(candidates, excludeNames)
	}
	if len(candidates) > 0 {
		return bestCandidate(day, candidates, preferredCuisine, profile).Meal, nil
	}

	if p.ai != nil {
		generated, err := p.ai.GenerateMeal(ctx, AIRequest{
			Date:             day,
			Slot:             slot,
			PreferredCuisine: preferredCuisine,
			Profile:          profile,
			BlockedTerms:     append(blockedTerms(profile), excludeNames...),
		})
		if err == nil && generated.Meal.Name != "" {
			_ = p.library.InsertGenerated(ctx, generated)
			return generated.Meal, nil
		}
		if err != nil {
			return models.Meal{}, err
		}
	}

	return fallbackMeal(slot, preferredCuisine).Meal, nil
}

func (p Planner) pickAlternative(ctx context.Context, day string, slot models.MealSlot, preferredCuisine models.Cuisine, profile *models.HealthProfile, excludeNames []string, forceAI bool, requestID string, attempt int) (models.Meal, error) {
	if forceAI {
		return p.generateAI(ctx, day, slot, preferredCuisine, profile, excludeNames, requestID, attempt)
	}

	libraryMeals, err := p.library.FindBySlot(ctx, slot)
	if err != nil {
		return models.Meal{}, err
	}

	candidates := excludeMeals(filterCandidates(libraryMeals, preferredCuisine, profile, true), excludeNames)
	if len(candidates) == 0 {
		candidates = excludeMeals(filterCandidates(libraryMeals, preferredCuisine, profile, false), excludeNames)
	}
	if len(candidates) > 0 {
		return bestCandidate(day, candidates, preferredCuisine, profile).Meal, nil
	}

	if p.ai != nil {
		if meal, err := p.generateAI(ctx, day, slot, preferredCuisine, profile, excludeNames, requestID, attempt); err == nil {
			return meal, nil
		}
	}

	// No AI configured or AI failed. Return the best safe library option even if the user has seen it before.
	return p.pick(ctx, day, slot, preferredCuisine, profile, nil)
}

func (p Planner) generateAI(ctx context.Context, day string, slot models.MealSlot, preferredCuisine models.Cuisine, profile *models.HealthProfile, excludeNames []string, requestID string, attempt int) (models.Meal, error) {
	if p.ai == nil {
		return models.Meal{}, ErrAINotConfigured
	}

	exclusions := cleanNames(excludeNames)
	blocked := append(blockedTerms(profile), exclusions...)
	var lastErr error

	for retry := 0; retry < 3; retry++ {
		generated, err := p.ai.GenerateMeal(ctx, AIRequest{
			Date:             day,
			RequestID:        requestID,
			Attempt:          attempt + retry,
			Slot:             slot,
			PreferredCuisine: preferredCuisine,
			Profile:          profile,
			BlockedTerms:     blocked,
		})
		if err != nil {
			return models.Meal{}, err
		}
		if generated.Meal.Name == "" {
			return models.Meal{}, ErrAIEmptyMeal
		}
		if isExcludedName(generated.Meal.Name, exclusions) {
			lastErr = ErrAIRepeatedMeal
			blocked = append(blocked, generated.Meal.Name)
			continue
		}
		_ = p.library.InsertGenerated(ctx, generated)
		return generated.Meal, nil
	}

	return models.Meal{}, lastErr
}

func excludeMeals(items []models.MealLibraryItem, names []string) []models.MealLibraryItem {
	if len(names) == 0 {
		return items
	}
	excluded := make(map[string]struct{}, len(names))
	for _, name := range names {
		excluded[normalized(name)] = struct{}{}
	}
	filtered := make([]models.MealLibraryItem, 0, len(items))
	for _, item := range items {
		if _, ok := excluded[normalized(item.Meal.Name)]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func cleanNames(names []string) []string {
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		key := normalized(name)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, name)
	}
	return cleaned
}

func isExcludedName(name string, exclusions []string) bool {
	name = normalized(name)
	for _, excluded := range exclusions {
		if name == normalized(excluded) {
			return true
		}
	}
	return false
}

func filterCandidates(items []models.MealLibraryItem, preferredCuisine models.Cuisine, profile *models.HealthProfile, strictDislikes bool) []models.MealLibraryItem {
	candidates := make([]models.MealLibraryItem, 0)
	for _, item := range items {
		if hasBlockedAllergen(item, profile) {
			continue
		}
		if strictDislikes && hasDislikedFood(item, profile) {
			continue
		}
		if !supportsDiet(item, profile) {
			continue
		}
		if item.Meal.Cuisine == preferredCuisine || len(candidates) == 0 {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

func bestCandidate(day string, candidates []models.MealLibraryItem, preferredCuisine models.Cuisine, profile *models.HealthProfile) models.MealLibraryItem {
	best := candidates[0]
	bestScore := scoreMeal(best, preferredCuisine, profile) + varietyScore(day, best.Meal.Name)
	for _, candidate := range candidates[1:] {
		score := scoreMeal(candidate, preferredCuisine, profile) + varietyScore(day, candidate.Meal.Name)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func scoreMeal(item models.MealLibraryItem, preferredCuisine models.Cuisine, profile *models.HealthProfile) int {
	score := 100
	if item.Meal.Cuisine == preferredCuisine {
		score += 80
	}

	goal := normalized(profileValue(profile, func(p *models.HealthProfile) string { return p.Goal }))
	activity := normalized(profileValue(profile, func(p *models.HealthProfile) string { return p.Activity }))

	switch {
	case strings.Contains(goal, "muscle"):
		score += proteinScore(item.Meal.Protein)
	case strings.Contains(goal, "weight"), strings.Contains(goal, "clean"):
		if item.Meal.Calories <= 540 {
			score += 25
		}
		if hasAnyTag(item.Meal, "Low sugar", "Low oil", "Light dinner", "Whole grain", "Fiber rich", "High fiber") {
			score += 20
		}
	case strings.Contains(goal, "energy"):
		if hasAnyTag(item.Meal, "Whole grain", "Balanced carbs", "Slow carbs", "Energy steady") {
			score += 25
		}
	}

	switch {
	case strings.Contains(activity, "very"):
		if item.Meal.Calories >= 520 {
			score += 20
		}
	case strings.Contains(activity, "light"):
		if item.Meal.Calories <= 520 {
			score += 20
		}
	}

	if hasAnyTag(item.Meal, "Balanced", "Heart healthy", "Heart friendly", "Vegetable packed", "Fiber rich") {
		score += 10
	}
	return score
}

func hasBlockedAllergen(item models.MealLibraryItem, profile *models.HealthProfile) bool {
	if profile == nil {
		return false
	}
	return containsAny(searchText(item), append(profile.Allergies, allergyAliases(profile.Allergies...)...))
}

func hasDislikedFood(item models.MealLibraryItem, profile *models.HealthProfile) bool {
	if profile == nil {
		return false
	}
	return containsAny(searchText(item), profile.DislikedFoods)
}

func supportsDiet(item models.MealLibraryItem, profile *models.HealthProfile) bool {
	diet := normalized(profileValue(profile, func(p *models.HealthProfile) string { return p.Diet }))
	if diet == "" || strings.Contains(diet, "no restriction") {
		return true
	}
	for _, supported := range item.Diets {
		if normalized(supported) == diet {
			return true
		}
	}
	return false
}

func searchText(item models.MealLibraryItem) string {
	parts := []string{
		item.Meal.Name,
		item.Meal.Description,
		strings.Join(item.Meal.Tags, " "),
		strings.Join(item.Ingredients, " "),
		strings.Join(item.Allergens, " "),
	}
	return normalized(strings.Join(parts, " "))
}

func containsAny(text string, values []string) bool {
	for _, value := range values {
		value = normalized(value)
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func blockedTerms(profile *models.HealthProfile) []string {
	if profile == nil {
		return nil
	}
	values := append([]string{}, profile.Allergies...)
	values = append(values, profile.DislikedFoods...)
	values = append(values, allergyAliases(profile.Allergies...)...)
	return values
}

func allergyAliases(values ...string) []string {
	aliases := make([]string, 0)
	for _, value := range values {
		switch normalized(value) {
		case "lactose", "milk":
			aliases = append(aliases, "yogurt", "cheese", "dairy")
		case "peanut", "peanuts":
			aliases = append(aliases, "peanut", "peanuts")
		case "shellfish":
			aliases = append(aliases, "shrimp", "prawn", "shellfish")
		case "gluten":
			aliases = append(aliases, "bread", "flatbread", "barley", "couscous")
		}
	}
	return aliases
}

func profileValue(profile *models.HealthProfile, getter func(*models.HealthProfile) string) string {
	if profile == nil {
		return ""
	}
	return getter(profile)
}

func normalized(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func hasAnyTag(meal models.Meal, tags ...string) bool {
	for _, tag := range tags {
		if slices.Contains(meal.Tags, tag) {
			return true
		}
	}
	return false
}

func proteinScore(value string) int {
	grams, err := strconv.Atoi(strings.TrimSuffix(value, "g"))
	if err != nil {
		return 0
	}
	switch {
	case grams >= 40:
		return 35
	case grams >= 30:
		return 25
	case grams >= 20:
		return 10
	default:
		return 0
	}
}

func varietyScore(day, mealName string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(day + mealName))
	return int(hash.Sum32() % 30)
}

func fallbackMeal(slot models.MealSlot, cuisine models.Cuisine) models.MealLibraryItem {
	return models.MealLibraryItem{
		Meal: models.Meal{
			Slot:        slot,
			Cuisine:     cuisine,
			Name:        "Balanced chef bowl",
			Description: "Lean protein, vegetables, whole grains, and a light sauce.",
			Calories:    520,
			Protein:     "32g",
			PrepTime:    "25 min",
			Tags:        []string{"Balanced", "Healthy", "Simple"},
		},
		Ingredients: []string{"vegetables", "whole grains"},
		Diets:       []string{"no restriction"},
		Source:      "fallback",
	}
}
