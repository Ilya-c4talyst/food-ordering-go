// Package testutil contains helpers used only by tests.
package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func Request(t *testing.T, method, url, body string, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d, want %d, body %s", method, url, response.StatusCode, wantStatus, data)
	}
	return data
}

func Object(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
