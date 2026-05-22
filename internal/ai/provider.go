package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mealapp-backend/internal/mealplanner"
	"mealapp-backend/internal/models"
)

type OpenAIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	if strings.TrimSpace(model) == "" {
		model = "gpt-5.2"
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *OpenAIProvider) GenerateMeal(ctx context.Context, request mealplanner.AIRequest) (models.MealLibraryItem, error) {
	if p == nil {
		return models.MealLibraryItem{}, errors.New("ai provider is not configured")
	}

	payload := responseRequest{
		Model:        p.model,
		Instructions: "Generate one healthy meal as strict JSON. This is a request for a new alternative meal, so create a different meal name and concept every time. Avoid every blocked ingredient and do not repeat any blocked meal name. Keep the meal realistic, culturally aligned with the requested cuisine, and suitable for the user's profile. Return only fields required by the schema.",
		Input:        buildPrompt(request),
		Text: responseText{
			Format: responseFormat{
				Type:        "json_schema",
				Name:        "generated_meal",
				Description: "A personalized meal library item.",
				Strict:      true,
				Schema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required": []string{
						"name", "description", "calories", "protein", "prepTime", "tags", "ingredients", "diets", "allergens",
					},
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"calories":    map[string]any{"type": "integer", "minimum": 250, "maximum": 800},
						"protein":     map[string]any{"type": "string"},
						"prepTime":    map[string]any{"type": "string"},
						"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"ingredients": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"diets":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"allergens":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return models.MealLibraryItem{}, err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(body))
	if err != nil {
		return models.MealLibraryItem{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(httpRequest)
	if err != nil {
		return models.MealLibraryItem{}, err
	}
	defer response.Body.Close()

	var decoded responseBody
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return models.MealLibraryItem{}, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return models.MealLibraryItem{}, fmt.Errorf("openai request failed: %s", decoded.Error.Message)
	}

	text := decoded.OutputText()
	var generated generatedMeal
	if err := json.Unmarshal([]byte(text), &generated); err != nil {
		return models.MealLibraryItem{}, err
	}

	return models.MealLibraryItem{
		Meal: models.Meal{
			Slot:        request.Slot,
			Cuisine:     request.PreferredCuisine,
			Name:        generated.Name,
			Description: generated.Description,
			Calories:    generated.Calories,
			Protein:     generated.Protein,
			PrepTime:    generated.PrepTime,
			Tags:        generated.Tags,
		},
		Ingredients: generated.Ingredients,
		Diets:       generated.Diets,
		Allergens:   generated.Allergens,
		Source:      "ai",
	}, nil
}

func buildPrompt(request mealplanner.AIRequest) string {
	profile := request.Profile
	if profile == nil {
		profile = &models.HealthProfile{Diet: "No restriction"}
	}
	return fmt.Sprintf(
		"Alternative request ID: %s\nAlternative attempt: %d\nDate: %s\nMeal slot: %s\nCuisine: %s\nGoal: %s\nDiet: %s\nActivity: %s\nAllergies: %s\nDisliked foods: %s\nDo not use or repeat these blocked ingredients or meal names: %s",
		request.RequestID,
		request.Attempt,
		request.Date,
		request.Slot,
		request.PreferredCuisine,
		profile.Goal,
		profile.Diet,
		profile.Activity,
		strings.Join(profile.Allergies, ", "),
		strings.Join(profile.DislikedFoods, ", "),
		strings.Join(request.BlockedTerms, ", "),
	)
}

type responseRequest struct {
	Model        string       `json:"model"`
	Instructions string       `json:"instructions"`
	Input        string       `json:"input"`
	Text         responseText `json:"text"`
}

type responseText struct {
	Format responseFormat `json:"format"`
}

type responseFormat struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Strict      bool           `json:"strict"`
	Schema      map[string]any `json:"schema"`
}

type responseBody struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r responseBody) OutputText() string {
	var parts []string
	for _, output := range r.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

type generatedMeal struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Calories    int      `json:"calories"`
	Protein     string   `json:"protein"`
	PrepTime    string   `json:"prepTime"`
	Tags        []string `json:"tags"`
	Ingredients []string `json:"ingredients"`
	Diets       []string `json:"diets"`
	Allergens   []string `json:"allergens"`
}
