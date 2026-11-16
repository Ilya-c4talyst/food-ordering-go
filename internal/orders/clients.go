package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// These DTOs describe HTTP contracts, not access to another service's database.
type dishDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Available  bool   `json:"available"`
}

func (a *api) exchange(ctx context.Context, method, url string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 201 {
		return response.StatusCode, fmt.Errorf("dependency returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(output); err != nil {
		return response.StatusCode, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return response.StatusCode, fmt.Errorf("invalid dependency response")
	}
	return response.StatusCode, nil
}

func (a *api) dish(ctx context.Context, id int64) (dishDTO, int, error) {
	var dish dishDTO
	status, err := a.exchange(ctx, "GET", a.menuURL+"/dishes/"+strconv.FormatInt(id, 10), nil, &dish)
	if err == nil && (status != 200 || dish.ID != id || dish.Name == "" || dish.PriceCents <= 0 || dish.PriceCents > 100000000) {
		err = fmt.Errorf("invalid dish response")
	}
	return dish, status, err
}

func (a *api) notify(ctx context.Context, order Order) error {
	input := struct {
		OrderID int64  `json:"order_id"`
		Message string `json:"message"`
	}{order.ID, fmt.Sprintf("Заказ №%d принят", order.ID)}
	var response struct {
		ID      int64 `json:"id"`
		OrderID int64 `json:"order_id"`
	}
	status, err := a.exchange(ctx, "POST", a.notificationURL+"/notifications", input, &response)
	if err != nil {
		return err
	}
	if status != 201 || response.ID <= 0 || response.OrderID != order.ID {
		return fmt.Errorf("invalid notification acknowledgement")
	}
	return nil
}
