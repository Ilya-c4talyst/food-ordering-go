package notifications

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"food-ordering/internal/platform"
	"food-ordering/internal/testutil"
)

func TestNotificationCRUDAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.db")
	db, err := platform.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(New(db))
	got := testutil.Object(t, testutil.Request(t, "POST", s.URL+"/notifications", `{"order_id":7,"message":"Заказ принят"}`, 201))
	id := int(got["id"].(float64))
	url := fmt.Sprintf("%s/notifications/%d", s.URL, id)
	testutil.Request(t, "PUT", url, `{"message":"Заказ подтвержден"}`, 200)
	s.Close()
	db.Close()
	db, err = platform.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	s = httptest.NewServer(New(db))
	defer s.Close()
	url = fmt.Sprintf("%s/notifications/%d", s.URL, id)
	got = testutil.Object(t, testutil.Request(t, "GET", url, "", 200))
	if got["message"] != "Заказ подтвержден" || got["order_id"] != float64(7) {
		t.Fatalf("unexpected notification: %v", got)
	}
	testutil.Request(t, "GET", s.URL+"/notifications", "", 200)
	testutil.Request(t, "DELETE", url, "", 204)
	testutil.Request(t, "GET", url, "", 404)
	testutil.Request(t, "PUT", url, `{"message":"test"}`, 404)
}

func TestNotificationValidation(t *testing.T) {
	db, err := platform.OpenDB(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(New(db))
	defer s.Close()
	for _, body := range []string{`null`, `{}`, `{"order_id":0,"message":"x"}`, `{"order_id":1,"message":" "}`, `{"order_id":1,"message":"x","extra":true}`, `{"order_id":1,"message":"x"} {}`} {
		testutil.Request(t, "POST", s.URL+"/notifications", body, 400)
	}
	testutil.Request(t, "GET", s.URL+"/notifications/abc", "", 400)
	testutil.Request(t, "DELETE", s.URL+"/notifications/1", "", 404)
	testutil.Request(t, "GET", s.URL+"/health", "", 200)
}
