package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/GeoffRiley/chippy-boot-dev/internal/auth"
	"github.com/GeoffRiley/chippy-boot-dev/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) handlerMetrics(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Add("Content-Type", "text/html")
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte(fmt.Sprintf(`
<html>

<body>
	<h1>Welcome, Chirpy Admin</h1>
	<p>Chirpy has been visited %d times!</p>
</body>

</html>
	`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) handlerReset(writer http.ResponseWriter, request *http.Request) {
	if cfg.platform != "dev" {
		writer.WriteHeader(http.StatusForbidden)
		writer.Write([]byte("Reset is only allowed in dev environment."))
		return
	}

	cfg.fileserverHits.Store(0)
	cfg.db.DeleteUsers(request.Context())
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("Hits reset to 0 and database reset to initial state."))
}

func (cfg *apiConfig) handlerPostChirps(writer http.ResponseWriter, request *http.Request) {
	type parameters struct {
		Body string `json:"body"`
	}

	token, err := auth.GetBearerToken(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	decoder := json.NewDecoder(request.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	cleaned, err := validateChirp(params.Body)
	if err != nil {
		respondWithError(writer, http.StatusBadRequest, err.Error(), err)
		return
	}

	chirp, err := cfg.db.CreateChirp(request.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: userID,
	})
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't create chirp", err)
		return
	}

	respondWithJSON(writer, http.StatusCreated, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

func (cfg *apiConfig) handlerUsersCreate(writer http.ResponseWriter, request *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	type response struct {
		User
	}

	if cfg.platform != "dev" {
		respondWithError(writer, http.StatusForbidden, "Users can only be created in dev environment.", nil)
		return
	}

	decoder := json.NewDecoder(request.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't hash password", err)
		return
	}

	user, err := cfg.db.CreateUser(request.Context(),
		database.CreateUserParams{
			Email:          params.Email,
			HashedPassword: hashedPassword,
		})
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't create user", err)
		return
	}

	respondWithJSON(writer, http.StatusCreated, response{
		User: User{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		},
	})
}

func (cfg *apiConfig) handlerUsersUpdate(writer http.ResponseWriter, request *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	type response struct {
		User
	}

	token, err := auth.GetBearerToken(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	decoder := json.NewDecoder(request.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't hash password", err)
		return
	}

	user, err := cfg.db.UpdateUser(request.Context(), database.UpdateUserParams{
		ID:             userID,
		Email:          params.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't update user", err)
		return
	}

	respondWithJSON(writer, http.StatusOK, response{
		User: User{
			ID:          user.ID,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
			Email:       user.Email,
			IsChirpyRed: user.IsChirpyRed,
		},
	})
}

func (cfg *apiConfig) handlerGetChirps(writer http.ResponseWriter, request *http.Request) {
	dbChirps, err := cfg.db.GetChirps(request.Context())
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't retrieve chirps", err)
		return
	}

	chirps := []Chirp{}
	for _, dbChirp := range dbChirps {
		chirps = append(chirps, Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			UserID:    dbChirp.UserID,
			Body:      dbChirp.Body,
		})
	}

	respondWithJSON(writer, http.StatusOK, chirps)
}

func (cfg *apiConfig) handlerRetrieveChirps(writer http.ResponseWriter, request *http.Request) {
	dbChirps, err := cfg.db.GetChirps(request.Context())
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't retrieve chirps", err)
		return
	}

	authorID := uuid.Nil
	authorIDString := request.URL.Query().Get("author_id")
	if authorIDString != "" {
		authorID, err = uuid.Parse(authorIDString)
		if err != nil {
			respondWithError(writer, http.StatusBadRequest, "Invalid author ID", err)
			return
		}
	}

	sortDirection := "asc"
	sortDirectionParam := request.URL.Query().Get("sort")
	if sortDirectionParam == "desc" {
		sortDirection = "desc"
	}

	chirps := []Chirp{}
	for _, dbChirp := range dbChirps {
		if authorID != uuid.Nil && dbChirp.UserID != authorID {
			continue
		}

		chirps = append(chirps, Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			UserID:    dbChirp.UserID,
			Body:      dbChirp.Body,
		})
	}

	sort.Slice(chirps, func(i, j int) bool {
		if sortDirection == "desc" {
			return chirps[i].CreatedAt.After(chirps[j].CreatedAt)
		}
		return chirps[i].CreatedAt.Before(chirps[j].CreatedAt)
	})

	respondWithJSON(writer, http.StatusOK, chirps)
}

func (cfg *apiConfig) handlerGetChirp(writer http.ResponseWriter, request *http.Request) {
	chirpIDString := request.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(writer, http.StatusBadRequest, "Invalid chirp ID", err)
		return
	}

	dbChirp, err := cfg.db.GetChirp(request.Context(), chirpID)
	if err != nil {
		respondWithError(writer, http.StatusNotFound, "Couldn't get chirp", err)
		return
	}

	respondWithJSON(writer, http.StatusOK, Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		UserID:    dbChirp.UserID,
		Body:      dbChirp.Body,
	})
}

func (cfg *apiConfig) handlerDeleteChirp(writer http.ResponseWriter, request *http.Request) {
	chirpIDString := request.PathValue("chirpID")
	chirpID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(writer, http.StatusBadRequest, "Invalid chirp ID", err)
		return
	}

	token, err := auth.GetBearerToken(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	dbChirp, err := cfg.db.GetChirp(request.Context(), chirpID)
	if err != nil {
		respondWithError(writer, http.StatusNotFound, "Couldn't get chirp", err)
		return
	}
	if dbChirp.UserID != userID {
		respondWithError(writer, http.StatusForbidden, "You can't delete this chirp", err)
		return
	}

	err = cfg.db.DeleteChirp(request.Context(), chirpID)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't delete chirp", err)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) handlerLogin(writer http.ResponseWriter, request *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	type response struct {
		User
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}

	decoder := json.NewDecoder(request.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	user, err := cfg.db.GetUserByEmail(request.Context(), params.Email)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Incorrect email or password", err)
		return
	}

	err = auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Incorrect email or password", err)
		return
	}

	accessToken, err := auth.MakeJWT(
		user.ID,
		cfg.jwtSecret,
		time.Hour,
	)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't create access JWT", err)
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't create refresh token", err)
		return
	}

	_, err = cfg.db.CreateRefreshToken(request.Context(), database.CreateRefreshTokenParams{
		UserID:    user.ID,
		Token:     refreshToken,
		ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 30),
	})
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't save refresh token", err)
		return
	}

	respondWithJSON(writer, http.StatusOK, response{
		User: User{
			ID:          user.ID,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
			Email:       user.Email,
			IsChirpyRed: user.IsChirpyRed,
		},
		Token:        accessToken,
		RefreshToken: refreshToken,
	})
}

func (cfg *apiConfig) handlerRefresh(writer http.ResponseWriter, request *http.Request) {
	type response struct {
		Token string `json:"token"`
	}

	refreshToken, err := auth.GetBearerToken(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusBadRequest, "Couldn't find token", err)
		return
	}

	user, err := cfg.db.GetUserFromRefreshToken(request.Context(), refreshToken)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't get user for refresh token", err)
		return
	}

	accessToken, err := auth.MakeJWT(
		user.ID,
		cfg.jwtSecret,
		time.Hour,
	)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't validate token", err)
		return
	}

	respondWithJSON(writer, http.StatusOK, response{
		Token: accessToken,
	})
}

func (cfg *apiConfig) handlerRevoke(writer http.ResponseWriter, request *http.Request) {
	refreshToken, err := auth.GetBearerToken(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusBadRequest, "Couldn't find token", err)
		return
	}

	_, err = cfg.db.RevokeRefreshToken(request.Context(), refreshToken)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't revoke session", err)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) handlerWebhooks(writer http.ResponseWriter, request *http.Request) {
	type parameters struct {
		Event string `json:"event"`
		Data  struct {
			UserID uuid.UUID `json:"user_id"`
		}
	}

	apiKey, err := auth.GetAPIKey(request.Header)
	if err != nil {
		respondWithError(writer, http.StatusUnauthorized, "Couldn't find api key", err)
		return
	}
	if apiKey != cfg.polkaKey {
		respondWithError(writer, http.StatusUnauthorized, "API key is invalid", err)
		return
	}

	decoder := json.NewDecoder(request.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(writer, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	if params.Event != "user.upgraded" {
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	_, err = cfg.db.UpgradeToChirpyRed(request.Context(), params.Data.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(writer, http.StatusNotFound, "Couldn't find user", err)
			return
		}
		respondWithError(writer, http.StatusInternalServerError, "Couldn't update user", err)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}
