package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestVerifyCaptchaSendsWidgetRecordID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/captcha/verify" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["widget_record_id"] != "widget-123" || payload["token"] != "token-123" {
			t.Errorf("captcha payload = %v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"data":{}}`))
	}))
	defer upstream.Close()

	client := &InfraiClient{BaseURL: upstream.URL}
	if err := client.VerifyCaptcha(context.Background(), "widget-123", "token-123", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCaptchaLive(t *testing.T) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		t.Skip("INFRAI_API_KEY is required for live verification")
	}
	err := (&InfraiClient{APIKey: key}).VerifyCaptcha(context.Background(), "codecheck-invalid-widget", "codecheck-invalid-token", "127.0.0.1")
	if err == nil {
		t.Fatal("invalid test captcha unexpectedly verified")
	}
	if strings.Contains(strings.ToLower(err.Error()), "widget_record_id") {
		t.Fatalf("widget_record_id was rejected: %v", err)
	}
	t.Logf("live captcha response: %v", err)
}
