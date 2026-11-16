package menu

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"food-ordering/internal/platform"
	"food-ordering/internal/testutil"
)

func TestDishCRUDAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "menu.db")
	db, err := platform.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(db))
	base := server.URL
	body := testutil.Request(t, "POST", base+"/dishes", `{"name":"Суп","price_cents":20000,"available":true}`, 201)
	id := int(testutil.Object(t, body)["id"].(float64))
	url := fmt.Sprintf("%s/dishes/%d", base, id)
	testutil.Request(t, "PUT", url, `{"name":"Суп дня","price_cents":22000,"available":false}`, 200)
	got := testutil.Object(t, testutil.Request(t, "GET", url, "", 200))
	if got["name"] != "Суп дня" || got["price_cents"] != float64(22000) || got["available"] != false {
		t.Fatalf("unexpected dish: %v", got)
	}
	// A deleted seed must not reappear when the schema is initialized again.
	testutil.Request(t, "DELETE", base+"/dishes/1", "", 204)
	server.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = platform.OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	server = httptest.NewServer(New(db))
	defer server.Close()
	testutil.Request(t, "GET", server.URL+"/dishes/1", "", 404)
	testutil.Request(t, "GET", fmt.Sprintf("%s/dishes/%d", server.URL, id), "", 200)
	testutil.Request(t, "DELETE", fmt.Sprintf("%s/dishes/%d", server.URL, id), "", 204)
	testutil.Request(t, "DELETE", fmt.Sprintf("%s/dishes/%d", server.URL, id), "", 404)
}

func TestDishValidation(t *testing.T) {
	db, err := platform.OpenDB(filepath.Join(t.TempDir(), "menu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(New(db))
	defer s.Close()
	for _, body := range []string{
		`{`, `null`, `[]`, `{} {}`, `{"name":" ","price_cents":1,"available":true}`,
		`{"name":"Суп","price_cents":0,"available":true}`,
		`{"name":"Суп","price_cents":-1,"available":true}`,
		`{"name":"Суп","price_cents":1.5,"available":true}`,
		`{"name":"Суп","price_cents":1,"available":null}`,
		`{"name":"Суп","price_cents":1,"extra":1,"available":true}`,
		`{"name":"Суп","price_cents":100000001,"available":true}`,
	} {
		testutil.Request(t, "POST", s.URL+"/dishes", body, 400)
	}
	for _, id := range []string{"0", "-1", "abc"} {
		testutil.Request(t, "GET", s.URL+"/dishes/"+id, "", 400)
	}
	testutil.Request(t, "GET", s.URL+"/dishes/999", "", 404)
	testutil.Request(t, "PATCH", s.URL+"/dishes/1", `{}`, 405)
	testutil.Request(t, "GET", s.URL+"/health", "", 200)
}

func TestConcurrentDishWrites(t *testing.T) {
	db, err := platform.OpenDB(filepath.Join(t.TempDir(), "menu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Init(db); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(New(db))
	defer s.Close()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testutil.Request(t, "POST", s.URL+"/dishes", `{"name":"Суп","price_cents":100,"available":true}`, 201)
		}()
	}
	wg.Wait()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM dishes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 13 {
		t.Fatalf("count = %d, want 13", count)
	}
}
