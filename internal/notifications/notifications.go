package notifications

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"food-ordering/internal/platform"
)

type Notification struct {
	ID        int64  `json:"id"`
	OrderID   int64  `json:"order_id"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

func Init(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS notifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL CHECK(order_id>0),
		message TEXT NOT NULL, created_at TEXT NOT NULL)`)
	return err
}

type api struct{ db *sql.DB }

func New(db *sql.DB) http.Handler {
	a := &api{db: db}
	mux := http.NewServeMux()
	platform.Health(mux, db)
	mux.HandleFunc("GET /notifications", a.list)
	mux.HandleFunc("POST /notifications", a.create)
	mux.HandleFunc("GET /notifications/{id}", a.get)
	mux.HandleFunc("PUT /notifications/{id}", a.update)
	mux.HandleFunc("DELETE /notifications/{id}", a.delete)
	return mux
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,order_id,message,created_at FROM notifications ORDER BY id")
	if err != nil {
		platform.DBError(w, err)
		return
	}
	defer rows.Close()
	items := []Notification{}
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.OrderID, &item.Message, &item.CreatedAt); err != nil {
			platform.DBError(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		platform.DBError(w, err)
		return
	}
	platform.JSON(w, 200, items)
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	var item Notification
	err := a.db.QueryRowContext(r.Context(), "SELECT id,order_id,message,created_at FROM notifications WHERE id=?", id).Scan(&item.ID, &item.OrderID, &item.Message, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		platform.Error(w, 404, "notification not found")
		return
	}
	if err != nil {
		platform.DBError(w, err)
		return
	}
	platform.JSON(w, 200, item)
}

func validMessage(message string) bool {
	return message != "" && utf8.RuneCountInString(message) <= 500
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OrderID int64  `json:"order_id"`
		Message string `json:"message"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	if input.OrderID <= 0 || !validMessage(input.Message) {
		platform.Error(w, 400, "positive order_id and message (1..500 characters) are required")
		return
	}
	item := Notification{OrderID: input.OrderID, Message: input.Message, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	result, err := a.db.ExecContext(r.Context(), "INSERT INTO notifications(order_id,message,created_at) VALUES(?,?,?)", item.OrderID, item.Message, item.CreatedAt)
	if err != nil {
		platform.DBError(w, err)
		return
	}
	item.ID, err = result.LastInsertId()
	if err != nil {
		platform.DBError(w, err)
		return
	}
	log.Printf("notification order_id=%d message=%q", item.OrderID, item.Message)
	platform.JSON(w, 201, item)
}

func (a *api) update(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	var input struct {
		Message string `json:"message"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	if !validMessage(input.Message) {
		platform.Error(w, 400, "message must contain 1..500 characters")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE notifications SET message=? WHERE id=?", input.Message, id)
	if platform.Changed(w, result, err) {
		a.get(w, r)
	}
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM notifications WHERE id=?", id)
	if platform.Changed(w, result, err) {
		w.WriteHeader(204)
	}
}
