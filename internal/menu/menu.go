package menu

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"food-ordering/internal/platform"
)

type Dish struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Available  bool   `json:"available"`
}

type dishInput struct {
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Available  *bool  `json:"available"`
}

func Init(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existed int
	if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dishes'").Scan(&existed); err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS dishes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL, price_cents INTEGER NOT NULL CHECK(price_cents>0),
		available INTEGER NOT NULL CHECK(available IN (0,1)))`)
	if err != nil {
		return err
	}
	if existed == 0 {
		_, err = tx.Exec(`INSERT INTO dishes(name,price_cents,available) VALUES
			('Пицца Маргарита',45000,1),('Лимонад',15000,1),('Чай',12000,1)`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type api struct{ db *sql.DB }

func New(db *sql.DB) http.Handler {
	a := &api{db: db}
	mux := http.NewServeMux()
	platform.Health(mux, db)
	mux.HandleFunc("GET /dishes", a.list)
	mux.HandleFunc("POST /dishes", a.save)
	mux.HandleFunc("GET /dishes/{id}", a.get)
	mux.HandleFunc("PUT /dishes/{id}", a.save)
	mux.HandleFunc("DELETE /dishes/{id}", a.delete)
	return mux
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,name,price_cents,available FROM dishes ORDER BY id")
	if err != nil {
		platform.DBError(w, err)
		return
	}
	defer rows.Close()
	items := []Dish{}
	for rows.Next() {
		var item Dish
		if err := rows.Scan(&item.ID, &item.Name, &item.PriceCents, &item.Available); err != nil {
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
	var item Dish
	err := a.db.QueryRowContext(r.Context(), "SELECT id,name,price_cents,available FROM dishes WHERE id=?", id).Scan(&item.ID, &item.Name, &item.PriceCents, &item.Available)
	if errors.Is(err, sql.ErrNoRows) {
		platform.Error(w, 404, "dish not found")
		return
	}
	if err != nil {
		platform.DBError(w, err)
		return
	}
	platform.JSON(w, 200, item)
}

func (a *api) save(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == "PUT" {
		var ok bool
		id, ok = platform.ID(w, r)
		if !ok {
			return
		}
	}
	var input dishInput
	if !platform.Decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 100 || input.PriceCents < 1 || input.PriceCents > 100000000 || input.Available == nil {
		platform.Error(w, 400, "name, price_cents (1..100000000) and available are required")
		return
	}
	status := 200
	if r.Method == "POST" {
		result, err := a.db.ExecContext(r.Context(), "INSERT INTO dishes(name,price_cents,available) VALUES(?,?,?)", input.Name, input.PriceCents, *input.Available)
		if err != nil {
			platform.DBError(w, err)
			return
		}
		id, err = result.LastInsertId()
		if err != nil {
			platform.DBError(w, err)
			return
		}
		status = 201
	} else {
		result, err := a.db.ExecContext(r.Context(), "UPDATE dishes SET name=?,price_cents=?,available=? WHERE id=?", input.Name, input.PriceCents, *input.Available, id)
		if !platform.Changed(w, result, err) {
			return
		}
	}
	platform.JSON(w, status, Dish{id, input.Name, input.PriceCents, *input.Available})
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM dishes WHERE id=?", id)
	if platform.Changed(w, result, err) {
		w.WriteHeader(204)
	}
}
