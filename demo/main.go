// The demo exercises running services over HTTP. It does not access their DBs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var client = &http.Client{Timeout: 10 * time.Second}

func request(method, url, body string, want int) []byte {
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s %s\nHTTP %d\n%s\n", method, url, response.StatusCode, bytes.TrimSpace(data))
	if response.StatusCode != want {
		log.Fatalf("expected HTTP %d", want)
	}
	return data
}

func main() {
	mode := flag.String("mode", "success", "success, notification-outage or menu-outage")
	menuURL := flag.String("menu", "http://localhost:8081", "menu URL")
	orderURL := flag.String("orders", "http://localhost:8082", "orders URL")
	notificationURL := flag.String("notifications", "http://localhost:8083", "notifications URL")
	flag.Parse()
	body := `{"items":[{"dish_id":1,"quantity":2},{"dish_id":2,"quantity":1}]}`
	switch *mode {
	case "success", "notification-outage":
		request("GET", *menuURL+"/dishes", "", 200)
		data := request("POST", *orderURL+"/orders", body, 201)
		var order struct {
			ID           int64  `json:"id"`
			Total        int64  `json:"total_cents"`
			Notification string `json:"notification_status"`
		}
		if err := json.Unmarshal(data, &order); err != nil {
			log.Fatal(err)
		}
		wantNotification := "confirmed"
		if *mode == "notification-outage" {
			wantNotification = "unconfirmed"
		}
		if order.Total != 105000 || order.Notification != wantNotification {
			log.Fatal("unexpected order result (demo assumes initial menu prices)")
		}
		request("GET", *orderURL+"/orders/"+strconv.FormatInt(order.ID, 10), "", 200)
		if *mode == "success" {
			request("GET", *notificationURL+"/notifications", "", 200)
		}
	case "menu-outage":
		before := request("GET", *orderURL+"/orders", "", 200)
		request("POST", *orderURL+"/orders", body, 503)
		after := request("GET", *orderURL+"/orders", "", 200)
		if !bytes.Equal(before, after) {
			log.Fatal("orders changed during menu outage")
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(1)
	}
	fmt.Println("PASS")
}
