package main

import (
	"food-ordering/internal/menu"
	"food-ordering/internal/platform"
	"log"
)

func main() {
	db, err := platform.OpenDB(platform.Env("DB_PATH", "data/menu.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := menu.Init(db); err != nil {
		log.Fatal(err)
	}
	if err := platform.Serve("menu-service", platform.Env("PORT", "8081"), menu.New(db)); err != nil {
		log.Fatal(err)
	}
}
