package orders

import (
	"context"
	"database/sql"
)

type Item struct {
	DishID     int64  `json:"dish_id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Quantity   int64  `json:"quantity"`
}

type Order struct {
	ID                 int64  `json:"id"`
	Items              []Item `json:"items"`
	TotalCents         int64  `json:"total_cents"`
	Status             string `json:"status"`
	NotificationStatus string `json:"notification_status"`
	CreatedAt          string `json:"created_at"`
	Warning            string `json:"warning,omitempty"`
}

func Init(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT, total_cents INTEGER NOT NULL CHECK(total_cents>0),
		status TEXT NOT NULL, notification_status TEXT NOT NULL, created_at TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS order_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
		dish_id INTEGER NOT NULL, name TEXT NOT NULL, price_cents INTEGER NOT NULL CHECK(price_cents>0),
		quantity INTEGER NOT NULL CHECK(quantity>0))`)
	return err
}

func (a *api) save(ctx context.Context, order *Order) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO orders(total_cents,status,notification_status,created_at) VALUES(?,?,?,?)", order.TotalCents, order.Status, order.NotificationStatus, order.CreatedAt)
	if err != nil {
		return err
	}
	order.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	for _, item := range order.Items {
		_, err = tx.ExecContext(ctx, "INSERT INTO order_items(order_id,dish_id,name,price_cents,quantity) VALUES(?,?,?,?,?)", order.ID, item.DishID, item.Name, item.PriceCents, item.Quantity)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// One JOIN query avoids nested queries while the SQLite pool has one connection.
func (a *api) load(ctx context.Context, id int64) ([]Order, error) {
	query := `SELECT o.id,o.total_cents,o.status,o.notification_status,o.created_at,
		i.dish_id,i.name,i.price_cents,i.quantity FROM orders o JOIN order_items i ON i.order_id=o.id`
	args := []any{}
	if id > 0 {
		query += " WHERE o.id=?"
		args = append(args, id)
	}
	query += " ORDER BY o.id,i.id"
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Order{}
	for rows.Next() {
		var order Order
		var item Item
		if err := rows.Scan(&order.ID, &order.TotalCents, &order.Status, &order.NotificationStatus, &order.CreatedAt, &item.DishID, &item.Name, &item.PriceCents, &item.Quantity); err != nil {
			return nil, err
		}
		if len(result) == 0 || result[len(result)-1].ID != order.ID {
			if order.NotificationStatus != "confirmed" {
				order.Warning = notificationWarning
			}
			order.Items = []Item{}
			result = append(result, order)
		}
		last := &result[len(result)-1]
		last.Items = append(last.Items, item)
	}
	return result, rows.Err()
}
