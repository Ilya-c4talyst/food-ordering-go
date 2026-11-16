package orders

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"food-ordering/internal/platform"
)

const notificationWarning = "Order saved, notification delivery is unconfirmed. Do not recreate the order."

type api struct {
	db                       *sql.DB
	menuURL, notificationURL string
	client                   *http.Client
}

func New(db *sql.DB, menuURL, notificationURL string, client *http.Client) http.Handler {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	a := &api{db, strings.TrimRight(menuURL, "/"), strings.TrimRight(notificationURL, "/"), client}
	mux := http.NewServeMux()
	platform.Health(mux, db)
	mux.HandleFunc("GET /orders", a.list)
	mux.HandleFunc("POST /orders", a.create)
	mux.HandleFunc("GET /orders/{id}", a.get)
	mux.HandleFunc("PUT /orders/{id}", a.update)
	mux.HandleFunc("DELETE /orders/{id}", a.delete)
	return mux
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []struct {
			DishID   int64 `json:"dish_id"`
			Quantity int64 `json:"quantity"`
		} `json:"items"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	if len(input.Items) == 0 || len(input.Items) > 20 {
		platform.Error(w, 400, "order must contain 1..20 different dishes")
		return
	}
	seen := map[int64]bool{}
	for _, item := range input.Items {
		if item.DishID <= 0 || item.Quantity < 1 || item.Quantity > 100 || seen[item.DishID] {
			platform.Error(w, 400, "positive dish_id, quantity 1..100 and no duplicate dishes required")
			return
		}
		seen[item.DishID] = true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	order := Order{Items: []Item{}, Status: "created", NotificationStatus: "unconfirmed", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, item := range input.Items {
		dish, status, err := a.dish(ctx, item.DishID)
		if status == 404 {
			platform.Error(w, 400, "dish not found")
			return
		}
		if err != nil {
			log.Printf("menu unavailable: %v", err)
			platform.Error(w, 503, "menu service unavailable, order was not created")
			return
		}
		if !dish.Available {
			platform.Error(w, 400, "dish is not available")
			return
		}
		order.Items = append(order.Items, Item{dish.ID, dish.Name, dish.PriceCents, item.Quantity})
		order.TotalCents += dish.PriceCents * item.Quantity
	}
	// Never keep an SQL transaction open during an HTTP request.
	if err := a.save(ctx, &order); err != nil {
		platform.DBError(w, err)
		return
	}
	if err := a.notify(ctx, order); err != nil {
		log.Printf("order_id=%d notification unconfirmed: %v", order.ID, err)
	} else {
		// The order has already been committed even if the client disconnects.
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer updateCancel()
		if _, err := a.db.ExecContext(updateCtx, "UPDATE orders SET notification_status='confirmed' WHERE id=?", order.ID); err != nil {
			log.Printf("order_id=%d cannot persist notification acknowledgement: %v", order.ID, err)
		} else {
			order.NotificationStatus = "confirmed"
		}
	}
	if order.NotificationStatus != "confirmed" {
		order.Warning = notificationWarning
	}
	platform.JSON(w, 201, order)
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.load(r.Context(), 0)
	if err != nil {
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
	items, err := a.load(r.Context(), id)
	if err != nil {
		platform.DBError(w, err)
		return
	}
	if len(items) == 0 {
		platform.Error(w, 404, "order not found")
		return
	}
	platform.JSON(w, 200, items[0])
}

func (a *api) update(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !platform.Decode(w, r, &input) {
		return
	}
	if input.Status != "created" && input.Status != "preparing" && input.Status != "ready" && input.Status != "cancelled" {
		platform.Error(w, 400, "unknown order status")
		return
	}
	items, err := a.load(r.Context(), id)
	if err != nil {
		platform.DBError(w, err)
		return
	}
	if len(items) == 0 {
		platform.Error(w, 404, "order not found")
		return
	}
	previous := items[0].Status
	allowed := input.Status == previous || previous == "created" && (input.Status == "preparing" || input.Status == "cancelled") || previous == "preparing" && (input.Status == "ready" || input.Status == "cancelled")
	if !allowed {
		platform.Error(w, 409, "invalid status transition")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE orders SET status=? WHERE id=? AND status=?", input.Status, id, previous)
	if err != nil {
		platform.DBError(w, err)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		platform.DBError(w, err)
		return
	}
	if count == 0 {
		platform.Error(w, 409, "order changed, reload it before updating")
		return
	}
	a.get(w, r)
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.ID(w, r)
	if !ok {
		return
	}
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM orders WHERE id=?", id)
	if platform.Changed(w, result, err) {
		w.WriteHeader(204)
	}
}
