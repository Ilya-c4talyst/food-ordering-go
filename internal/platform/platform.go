// Package platform contains infrastructure helpers, not shared service data.
package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection is enough for the lab and serializes SQLite writes.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if value != nil {
		if err := json.NewEncoder(w).Encode(value); err != nil {
			log.Printf("encode response: %v", err)
		}
	}
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

func DBError(w http.ResponseWriter, err error) {
	log.Printf("database error: %v", err)
	Error(w, http.StatusInternalServerError, "database operation failed")
}

func Decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		Error(w, 400, "invalid JSON or unknown fields")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		Error(w, 400, "expected one JSON object")
		return false
	}
	return true
}

func ID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		Error(w, 400, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func Changed(w http.ResponseWriter, result sql.Result, err error) bool {
	if err != nil {
		DBError(w, err)
		return false
	}
	count, err := result.RowsAffected()
	if err != nil {
		DBError(w, err)
		return false
	}
	if count == 0 {
		Error(w, 404, "record not found")
		return false
	}
	return true
}

func Health(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			Error(w, 503, "database unavailable")
			return
		}
		JSON(w, 200, map[string]string{"status": "ok"})
	})
}

func Env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func Serve(service, port string, handler http.Handler) error {
	server := &http.Server{
		Addr: ":" + port, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s %s", service, r.Method, r.URL.Path)
			handler.ServeHTTP(w, r)
		}),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("%s listening on %s", service, server.Addr)
	err := server.ListenAndServe()
	stop()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
