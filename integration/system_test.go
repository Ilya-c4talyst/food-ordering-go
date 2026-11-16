package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"food-ordering/internal/menu"
	"food-ordering/internal/notifications"
	"food-ordering/internal/orders"
	"food-ordering/internal/platform"
	"food-ordering/internal/testutil"
)

const orderBody = `{"items":[{"dish_id":1,"quantity":2},{"dish_id":2,"quantity":1}]}`

type system struct {
	menu, notifications, orders *httptest.Server
	orderDB                     *sql.DB
}

func openDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := platform.OpenDB(filepath.Join(t.TempDir(), name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func start(t *testing.T) *system {
	t.Helper()
	mdb, ndb, odb := openDB(t, "menu"), openDB(t, "notifications"), openDB(t, "orders")
	for _, init := range []struct {
		fn func(*sql.DB) error
		db *sql.DB
	}{{menu.Init, mdb}, {notifications.Init, ndb}, {orders.Init, odb}} {
		if err := init.fn(init.db); err != nil {
			t.Fatal(err)
		}
	}
	m := httptest.NewServer(menu.New(mdb))
	n := httptest.NewServer(notifications.New(ndb))
	o := httptest.NewServer(orders.New(odb, m.URL, n.URL, &http.Client{Timeout: time.Second}))
	t.Cleanup(m.Close)
	t.Cleanup(n.Close)
	t.Cleanup(o.Close)
	return &system{m, n, o, odb}
}

func array(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestCreateOrderOverHTTPAndCRUD(t *testing.T) {
	s := start(t)
	got := testutil.Object(t, testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 201))
	if got["total_cents"] != float64(105000) || got["status"] != "created" || got["notification_status"] != "confirmed" {
		t.Fatalf("unexpected order: %v", got)
	}
	id := int(got["id"].(float64))
	url := fmt.Sprintf("%s/orders/%d", s.orders.URL, id)
	n := array(t, testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200))
	if len(n) != 1 || n[0]["order_id"] != float64(id) {
		t.Fatalf("notification not saved: %v", n)
	}
	// The price/name snapshot survives changing and then deleting the dish.
	testutil.Request(t, "PUT", s.menu.URL+"/dishes/1", `{"name":"Другая пицца","price_cents":99000,"available":true}`, 200)
	testutil.Request(t, "DELETE", s.menu.URL+"/dishes/1", "", 204)
	got = testutil.Object(t, testutil.Request(t, "GET", url, "", 200))
	first := got["items"].([]any)[0].(map[string]any)
	if got["total_cents"] != float64(105000) || first["name"] != "Пицца Маргарита" || first["price_cents"] != float64(45000) {
		t.Fatalf("snapshot changed: %v", got)
	}
	testutil.Request(t, "PUT", url, `{"status":"ready"}`, 409)
	testutil.Request(t, "PUT", url, `{"status":"preparing"}`, 200)
	testutil.Request(t, "PUT", url, `{"status":"ready"}`, 200)
	testutil.Request(t, "PUT", url, `{"status":"created"}`, 409)
	testutil.Request(t, "GET", s.orders.URL+"/orders", "", 200)
	testutil.Request(t, "DELETE", url, "", 204)
	testutil.Request(t, "GET", url, "", 404)
	testutil.Request(t, "PUT", url, `{"status":"preparing"}`, 404)
	testutil.Request(t, "DELETE", url, "", 404)
	// Deleting an order must not cascade into the other service's database.
	n = array(t, testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200))
	if len(n) != 1 {
		t.Fatalf("independent notification disappeared: %v", n)
	}
}

func TestNotificationOutageKeepsOrder(t *testing.T) {
	s := start(t)
	s.notifications.Close()
	got := testutil.Object(t, testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 201))
	if got["notification_status"] != "unconfirmed" || got["warning"] == nil {
		t.Fatalf("outage not reported: %v", got)
	}
	id := int(got["id"].(float64))
	got = testutil.Object(t, testutil.Request(t, "GET", fmt.Sprintf("%s/orders/%d", s.orders.URL, id), "", 200))
	if got["notification_status"] != "unconfirmed" || got["total_cents"] != float64(105000) {
		t.Fatalf("order was not persisted: %v", got)
	}
	testutil.Request(t, "GET", s.menu.URL+"/dishes", "", 200)
}

func TestMenuOutageRejectsOnlyNewOrders(t *testing.T) {
	s := start(t)
	testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 201)
	s.menu.Close()
	testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 503)
	got := array(t, testutil.Request(t, "GET", s.orders.URL+"/orders", "", 200))
	if len(got) != 1 {
		t.Fatalf("partial order saved: %v", got)
	}
	testutil.Request(t, "GET", s.orders.URL+"/orders/1", "", 200)
	testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200)
}

func TestOrderValidationDoesNotSave(t *testing.T) {
	s := start(t)
	testutil.Request(t, "PUT", s.menu.URL+"/dishes/3", `{"name":"Чай","price_cents":12000,"available":false}`, 200)
	bodies := []string{`null`, `{}`, `{"items":[]}`, `{"items":[{"dish_id":1,"quantity":0}]}`,
		`{"items":[{"dish_id":1,"quantity":101}]}`, `{"items":[{"dish_id":1,"quantity":-1}]}`,
		`{"items":[{"dish_id":0,"quantity":1}]}`, `{"items":[{"dish_id":1,"quantity":1.2}]}`,
		`{"items":[{"dish_id":1,"quantity":1,"price_cents":1}]}`,
		`{"items":[{"dish_id":999,"quantity":1}]}`, `{"items":[{"dish_id":3,"quantity":1}]}`,
		`{"items":[{"dish_id":1,"quantity":1},{"dish_id":999,"quantity":1}]}`,
		`{"items":[{"dish_id":1,"quantity":1},{"dish_id":1,"quantity":2}]}`,
		orderBody + ` {}`, `{"items":[{"dish_id":1,"quantity":1}],"total_cents":1}`,
		`{"items":[` + strings.Repeat(`{"dish_id":1,"quantity":1},`, 20) + `{"dish_id":2,"quantity":1}]}`,
	}
	for _, body := range bodies {
		testutil.Request(t, "POST", s.orders.URL+"/orders", body, 400)
	}
	if got := array(t, testutil.Request(t, "GET", s.orders.URL+"/orders", "", 200)); len(got) != 0 {
		t.Fatalf("invalid orders saved: %v", got)
	}
	testutil.Request(t, "GET", s.orders.URL+"/orders/abc", "", 400)
	testutil.Request(t, "GET", s.orders.URL+"/orders/0", "", 400)
	testutil.Request(t, "PUT", s.orders.URL+"/orders/1", `{"status":"invalid"}`, 400)
}

func TestOrderTransactionRollsBack(t *testing.T) {
	s := start(t)
	_, err := s.orderDB.Exec(`CREATE TRIGGER reject_items BEFORE INSERT ON order_items BEGIN SELECT RAISE(ABORT, 'test failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 500)
	if got := array(t, testutil.Request(t, "GET", s.orders.URL+"/orders", "", 200)); len(got) != 0 {
		t.Fatalf("transaction not rolled back: %v", got)
	}
	if got := array(t, testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200)); len(got) != 0 {
		t.Fatalf("notification sent before commit: %v", got)
	}
}

func TestConcurrentOrders(t *testing.T) {
	s := start(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); testutil.Request(t, "POST", s.orders.URL+"/orders", orderBody, 201) }()
	}
	wg.Wait()
	got := array(t, testutil.Request(t, "GET", s.orders.URL+"/orders", "", 200))
	if len(got) != 10 {
		t.Fatalf("lost orders: %d", len(got))
	}
	for _, item := range got {
		if item["total_cents"] != float64(105000) || item["notification_status"] != "confirmed" {
			t.Fatalf("broken order: %v", item)
		}
	}
	if n := array(t, testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200)); len(n) != 10 {
		t.Fatalf("lost notifications: %d", len(n))
	}
}

func TestBadMenuResponseAndTimeout(t *testing.T) {
	for _, mode := range []string{"invalid-json", "bad-price", "wrong-id", "http-error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			dependency := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "invalid-json":
					fmt.Fprint(w, `not json`)
				case "bad-price":
					fmt.Fprint(w, `{"id":1,"name":"x","price_cents":-1,"available":true}`)
				case "wrong-id":
					fmt.Fprint(w, `{"id":9,"name":"x","price_cents":100,"available":true}`)
				case "http-error":
					w.WriteHeader(500)
				case "timeout":
					time.Sleep(100 * time.Millisecond)
					fmt.Fprint(w, `{}`)
				}
			}))
			defer dependency.Close()
			db := openDB(t, "orders")
			if err := orders.Init(db); err != nil {
				t.Fatal(err)
			}
			s := httptest.NewServer(orders.New(db, dependency.URL, dependency.URL, &http.Client{Timeout: 20 * time.Millisecond}))
			defer s.Close()
			testutil.Request(t, "POST", s.URL+"/orders", `{"items":[{"dish_id":1,"quantity":1}]}`, 503)
			if got := array(t, testutil.Request(t, "GET", s.URL+"/orders", "", 200)); len(got) != 0 {
				t.Fatalf("saved on bad dependency: %v", got)
			}
		})
	}
}

func TestNotificationTimeoutHasUncertainResult(t *testing.T) {
	s := start(t)
	// Real receiver persists first, while the HTTP acknowledgement arrives too late.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := http.NewRequest("POST", s.notifications.URL+"/notifications", r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		response.Body.Close()
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(201)
	}))
	defer slow.Close()
	db := openDB(t, "orders")
	if err := orders.Init(db); err != nil {
		t.Fatal(err)
	}
	o := httptest.NewServer(orders.New(db, s.menu.URL, slow.URL, &http.Client{Timeout: 30 * time.Millisecond}))
	defer o.Close()
	got := testutil.Object(t, testutil.Request(t, "POST", o.URL+"/orders", orderBody, 201))
	if got["notification_status"] != "unconfirmed" {
		t.Fatalf("false confirmation: %v", got)
	}
	n := array(t, testutil.Request(t, "GET", s.notifications.URL+"/notifications", "", 200))
	if len(n) != 1 {
		t.Fatalf("expected notification despite lost acknowledgement: %v", n)
	}
}
