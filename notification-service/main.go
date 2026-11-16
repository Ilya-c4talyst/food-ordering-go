package main

import (
	"food-ordering/internal/notifications"
	"food-ordering/internal/platform"
	"log"
)

func main() {
	db, err := platform.OpenDB(platform.Env("DB_PATH", "data/notifications.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := notifications.Init(db); err != nil {
		log.Fatal(err)
	}
	if err := platform.Serve("notification-service", platform.Env("PORT", "8083"), notifications.New(db)); err != nil {
		log.Fatal(err)
	}
}
