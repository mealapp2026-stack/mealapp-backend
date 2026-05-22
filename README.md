# Mealapp Backend

Go and MongoDB API for the Surprise Chef mobile app.

## Run locally

1. Start MongoDB:

```bash
docker compose up -d
```

2. Copy `.env.example` values into your shell or a `.env` loader.
3. Start the API:

```bash
go run ./cmd/api
```

The API listens on `http://localhost:8080` by default.

## Deploy to Render

The repo includes a root `main.go`, so Render's default root build works:

```bash
go build -tags netgo -ldflags '-s -w' -o app
```

This package-specific build also works:

```bash
go build -tags netgo -ldflags '-s -w' -o app ./cmd/api
```

Use this start command:

```bash
./app
```

Required Render environment variables:

```text
APP_ENV=production
MONGO_URI=<your MongoDB Atlas connection string>
MONGO_DATABASE=mealapp
JWT_SECRET=<a long random secret>
TOKEN_TTL_HOURS=168
OPENAI_API_KEY=
OPENAI_MODEL=gpt-5.2
```

Render provides `PORT` automatically, and the backend will listen on that port when `HTTP_ADDR` is not set.

## Example requests

Register:

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"name":"Joseph","email":"joseph@example.com","password":"password123"}'
```

Save the returned token and call protected endpoints:

```bash
curl http://localhost:8080/api/v1/meal-plans/today \
  -H "Authorization: Bearer <token>"
```

## API

- `GET /health`
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/v1/me`
- `PUT /api/v1/me/profile`
- `GET /api/v1/meal-plans/today`
- `PUT /api/v1/meal-plans/preferences`

Protected endpoints require:

```http
Authorization: Bearer <token>
```

The meal planner uses a hybrid recommendation system without changing the mobile API contract.

## Meal suggestion logic

Meal suggestions are generated in `internal/mealplanner`.

The planner now:

- reads the user's saved breakfast, lunch, and dinner cuisine preferences
- loads candidate meals from the MongoDB `meals` collection
- filters unsafe meals using allergies
- avoids disliked foods when possible
- respects diet settings such as vegetarian, pescatarian, and low carb
- scores meals based on goal and activity level
- adds day-based variety so the same cuisine can rotate through different safe meals
- calls the AI provider only when the MongoDB library cannot provide a safe match
- stores AI-generated meals back into MongoDB for future reuse

After changing planner code, restart the backend process before testing from the app.

## AI setup

AI generation is optional. Without `OPENAI_API_KEY`, the backend still works from MongoDB seed meals and local fallback meals.

To enable OpenAI fallback generation:

```bash
export OPENAI_API_KEY="your-key"
export OPENAI_MODEL="gpt-5.2"
HTTP_ADDR=:8085 go run ./cmd/api
```

Seed meals are inserted automatically into the `meals` collection when the backend starts.
