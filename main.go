package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`
<html>

<body>
	<h1>Welcome, Chirpy Admin</h1>
	<p>Chirpy has been visited %d times!</p>
</body>

</html>
	`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	//if cfg.platform != "dev" {
	//	w.WriteHeader(http.StatusForbidden)
	//	w.Write([]byte("Reset is only allowed in dev environment."))
	//	return
	//}

	cfg.fileserverHits.Store(0)
	//cfg.db.Reset(r.Context())
	w.WriteHeader(http.StatusOK)
	//w.Write([]byte("Hits reset to 0 and database reset to initial state."))
	w.Write([]byte("Hits reset to 0."))
}

type chirpRequest struct {
	Body string `json:"body"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type validResponse struct {
	ValidBody bool `json:"valid"`
}

type cleanedResponse struct {
	CleanedBody string `json:"cleaned_body"`
}

var profaneWords = []string{"kerfuffle", "sharbert", "fornax"}

func cleanProfanity(body string) string {
	for _, word := range profaneWords {
		regex := regexp.MustCompile(`\b(?i)` + word + `\b`)
		body = regex.ReplaceAllString(body, "****")
	}
	return body
}

func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	var chirp chirpRequest

	if err := json.NewDecoder(r.Body).Decode(&chirp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse{Error: "Invalid JSON"})
		return
	}

	if len(chirp.Body) > 140 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse{Error: "Chirp is too long"})
		return
	}

	cleanedBody := cleanProfanity(chirp.Body)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(cleanedResponse{CleanedBody: cleanedBody})
	//json.NewEncoder(w).Encode(validResponse{ValidBody: true})
}

func main() {
	const port = "8080"

	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	fsHandlerApp := apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir("./public"))))
	mux.Handle("/app/", fsHandlerApp)
	fsHandlerAssets := apiCfg.middlewareMetricsInc(http.StripPrefix("/app/assets/", http.FileServer(http.Dir("./assets"))))
	mux.Handle("/app/assets/", fsHandlerAssets)

	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)

	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerMetrics)

	svr := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("Starting server on port %s", port)
	log.Fatal(svr.ListenAndServe())
}
