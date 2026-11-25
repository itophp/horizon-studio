package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"horizon/foundation/system/identity"
)

func smsContactDelivery(endpoint, token string) identity.ContactDelivery {
	if endpoint == "" || token == "" {
		return nil
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, destination, purpose, code, id string) error {
		body, err := json.Marshal(map[string]string{
			"to": destination, "purpose": purpose, "code": code, "messageId": id,
		})
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", id)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return errors.New("SMS provider rejected challenge")
		}
		return nil
	}
}
