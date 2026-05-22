package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"mealapp-backend/internal/auth"
	"mealapp-backend/internal/config"
	"mealapp-backend/internal/mealplanner"
	"mealapp-backend/internal/models"
	"mealapp-backend/internal/store"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type Server struct {
	config  config.Config
	users   *store.UserStore
	planner mealplanner.Planner
	mux     *http.ServeMux
}

type contextKey string

const userIDKey contextKey = "userID"

func NewServer(cfg config.Config, users *store.UserStore, planner mealplanner.Planner) *Server {
	server := &Server{
		config:  cfg,
		users:   users,
		planner: planner,
		mux:     http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /api/v1/auth/register", s.register)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.Handle("GET /api/v1/me", s.requireAuth(http.HandlerFunc(s.me)))
	s.mux.Handle("PUT /api/v1/me/profile", s.requireAuth(http.HandlerFunc(s.updateProfile)))
	s.mux.Handle("GET /api/v1/meal-plans/today", s.requireAuth(http.HandlerFunc(s.todayMealPlan)))
	s.mux.Handle("POST /api/v1/meal-plans/alternatives", s.requireAuth(http.HandlerFunc(s.alternativeMeal)))
	s.mux.Handle("PUT /api/v1/meal-plans/preferences", s.requireAuth(http.HandlerFunc(s.updatePreferences)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"env":          s.config.AppEnv,
		"aiConfigured": strings.TrimSpace(s.config.OpenAIAPIKey) != "",
		"aiModel":      s.config.OpenAIModel,
	})
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if request.Name == "" || request.Email == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "name, email, and password are required")
		return
	}

	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.users.Create(r.Context(), models.User{
		Name:         request.Name,
		Email:        request.Email,
		PasswordHash: passwordHash,
	})
	if mongo.IsDuplicateKeyError(err) {
		writeError(w, http.StatusConflict, "email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	s.writeAuthResponse(w, user)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	user, err := s.users.FindByEmail(r.Context(), request.Email)
	if errors.Is(err, store.ErrNotFound) || !auth.VerifyPassword(request.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to login")
		return
	}

	s.writeAuthResponse(w, user)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]models.User{"user": user})
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var profile models.HealthProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	normalizeProfile(&profile)

	user, err := s.users.UpdateProfile(r.Context(), userID, profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]models.User{"user": user})
}

func (s *Server) todayMealPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	plan, err := s.planner.Today(r.Context(), user.Preferences, user.Profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate meal plan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]models.DailyMealPlan{"mealPlan": plan})
}

type alternativeMealRequest struct {
	Slot             models.MealSlot `json:"slot"`
	CurrentMealName  string          `json:"currentMealName"`
	ExcludeMealNames []string        `json:"excludeMealNames"`
	ForceAI          bool            `json:"forceAI"`
	RequestID        string          `json:"requestID"`
	Attempt          int             `json:"attempt"`
}

func (s *Server) alternativeMeal(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)
	if !ok {
		return
	}

	var request alternativeMealRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !validMealSlot(request.Slot) {
		writeError(w, http.StatusBadRequest, "invalid meal slot")
		return
	}

	meal, err := s.planner.Alternative(
		r.Context(),
		request.Slot,
		preferredCuisineForSlot(user.Preferences, request.Slot),
		user.Profile,
		alternativeExclusions(request),
		false,
		request.RequestID,
		request.Attempt,
	)
	if err != nil {
		log.Printf("alternative meal failed: slot=%s current=%q exclusions=%v err=%v", request.Slot, request.CurrentMealName, alternativeExclusions(request), err)
		writeError(w, http.StatusInternalServerError, "alternative meal failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]models.Meal{"meal": meal})
}

func alternativeExclusions(request alternativeMealRequest) []string {
	exclusions := append([]string{}, request.ExcludeMealNames...)
	if request.CurrentMealName != "" {
		exclusions = append(exclusions, request.CurrentMealName)
	}
	return cleanStringSlice(exclusions)
}

func (s *Server) updatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var preferences models.MealPreferences
	if !decodeJSON(w, r, &preferences) {
		return
	}
	if !validCuisine(preferences.Breakfast) || !validCuisine(preferences.Lunch) || !validCuisine(preferences.Dinner) {
		writeError(w, http.StatusBadRequest, "invalid cuisine preference")
		return
	}

	user, err := s.users.UpdatePreferences(r.Context(), userID, preferences)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]models.User{"user": user})
}

func (s *Server) writeAuthResponse(w http.ResponseWriter, user models.User) {
	token, err := auth.SignToken(auth.Claims{
		Subject: user.ID.Hex(),
		Email:   user.Email,
		Expiry:  time.Now().Add(s.config.TokenTTL).Unix(),
	}, s.config.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  user,
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader || strings.TrimSpace(token) == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		claims, err := auth.VerifyToken(token, s.config.JWTSecret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		userID, err := primitive.ObjectIDFromHex(claims.Subject)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token subject")
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (models.User, bool) {
	userID, ok := currentUserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return models.User{}, false
	}

	user, err := s.users.FindByID(r.Context(), userID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "user not found")
		return models.User{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return models.User{}, false
	}
	return user, true
}

func currentUserID(ctx context.Context) (primitive.ObjectID, bool) {
	id, ok := ctx.Value(userIDKey).(primitive.ObjectID)
	return id, ok
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeProfile(profile *models.HealthProfile) {
	profile.Goal = strings.TrimSpace(profile.Goal)
	profile.Diet = strings.TrimSpace(profile.Diet)
	profile.Activity = strings.TrimSpace(profile.Activity)
	profile.Allergies = cleanStringSlice(profile.Allergies)
	profile.DislikedFoods = cleanStringSlice(profile.DislikedFoods)
}

func cleanStringSlice(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func validCuisine(cuisine models.Cuisine) bool {
	switch cuisine {
	case models.CuisineAfrican,
		models.CuisineFrench,
		models.CuisineChinese,
		models.CuisineMediterranean,
		models.CuisineIndian:
		return true
	default:
		return false
	}
}

func validMealSlot(slot models.MealSlot) bool {
	switch slot {
	case models.MealSlotBreakfast, models.MealSlotLunch, models.MealSlotDinner:
		return true
	default:
		return false
	}
}

func preferredCuisineForSlot(preferences models.MealPreferences, slot models.MealSlot) models.Cuisine {
	switch slot {
	case models.MealSlotBreakfast:
		return preferences.Breakfast
	case models.MealSlotLunch:
		return preferences.Lunch
	case models.MealSlotDinner:
		return preferences.Dinner
	default:
		return models.CuisineMediterranean
	}
}
