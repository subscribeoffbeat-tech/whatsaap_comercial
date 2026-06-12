package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"whatsapptool/internal/db"
)

// Health returns 200 {"status":"ok","quality":"green"} when the DB is reachable, 503 otherwise.
func Health(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
			return
		}
		resp := map[string]string{"status": "ok"}
		qi, err := db.GetQualityInfo(r.Context(), pool)
		if err == nil {
			resp["quality"] = qi.QualityRating
			resp["tier"] = "1000"
		}
		json.NewEncoder(w).Encode(resp)
	}
}
