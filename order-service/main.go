package main

import (
	"food-ordering/internal/orders"
	"food-ordering/internal/platform"
	"log"
	"net/http"
	"time"
)

func main() {
	db, err := platform.OpenDB(platform.Env("DB_PATH", "data/orders.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := orders.Init(db); err != nil {
		log.Fatal(err)
	}
	handler := orders.New(db, platform.Env("MENU_URL", "http://localhost:8081"), platform.Env("NOTIFICATION_URL", "http://localhost:8083"), &http.Client{Timeout: 2 * time.Second})
	if err := platform.Serve("order-service", platform.Env("PORT", "8082"), handler); err != nil {
		log.Fatal(err)
	}
}
