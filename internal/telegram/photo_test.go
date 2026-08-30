package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a *Client aimed at an httptest server. NewClient takes
// only a token and hardcodes the real Telegram base URL, so tests construct
// the struct directly (this file is package telegram, not telegram_test) to
// point baseURL at the test server instead.
func newTestClient(baseURL string) *Client {
	return &Client{
		token:      "test-token",
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestSendPhotoBytesReturnsTheLargestFileID(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"photo":[
			{"file_id":"small","width":90,"height":69},
			{"file_id":"large","width":500,"height":384}
		]}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	sent, err := client.SendPhotoBytes(context.Background(), 42, []byte("png-bytes"), "підпис", nil)
	if err != nil {
		t.Fatalf("SendPhotoBytes: %v", err)
	}
	if sent.MessageID != 77 {
		t.Fatalf("MessageID = %d, want 77", sent.MessageID)
	}
	if sent.FileID != "large" {
		t.Fatalf("FileID = %q, want the largest size file id", sent.FileID)
	}
	if !strings.HasSuffix(gotMethod, "/sendPhoto") {
		t.Fatalf("called %q, want sendPhoto", gotMethod)
	}
}

func TestSendPhotoRefSendsFileIDAsAPlainField(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5,"photo":[{"file_id":"reused","width":500,"height":384}]}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	sent, err := client.SendPhotoRef(context.Background(), 42, "reused", "підпис", nil)
	if err != nil {
		t.Fatalf("SendPhotoRef: %v", err)
	}
	if sent.FileID != "reused" {
		t.Fatalf("FileID = %q, want reused", sent.FileID)
	}
	if payload["photo"] != "reused" {
		t.Fatalf("payload photo = %v, want the raw file id string", payload["photo"])
	}
	if payload["caption"] != "підпис" {
		t.Fatalf("payload caption = %v, want the caption", payload["caption"])
	}
}

func TestSendPhotoBytesFailsLoudlyWhenTelegramReturnsNoPhoto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"photo":[]}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	_, err := client.SendPhotoBytes(context.Background(), 42, []byte("png"), "", nil)
	if err == nil {
		t.Fatal("SendPhotoBytes with an empty photo array succeeded, want an error")
	}
}

func TestEditMessageMediaRefSendsFileIDAndCaption(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9,"chat":{"id":42,"type":"private"}}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	err := client.EditMessageMediaRef(context.Background(), 42, 9, "reused-id", "нова підпис", nil)
	if err != nil {
		t.Fatalf("EditMessageMediaRef: %v", err)
	}
	media, ok := payload["media"].(map[string]any)
	if !ok {
		t.Fatalf("payload media = %v, want an object", payload["media"])
	}
	if media["media"] != "reused-id" {
		t.Fatalf("media.media = %v, want the file id", media["media"])
	}
	if media["caption"] != "нова підпис" {
		t.Fatalf("media.caption = %v, want the caption", media["caption"])
	}
	if media["type"] != "photo" {
		t.Fatalf("media.type = %v, want photo", media["type"])
	}
}
