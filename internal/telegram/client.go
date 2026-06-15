package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		baseURL:    "https://api.telegram.org",
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error {
	payload := map[string]any{"chat_id": chatID, "text": text}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "sendMessage", payload, nil)
}

func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "editMessageText", payload, nil)
}

func (c *Client) EditMessageCaption(ctx context.Context, chatID, messageID int64, caption string, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"caption":    caption,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "editMessageCaption", payload, nil)
}

func (c *Client) EditMessageReplyMarkup(ctx context.Context, chatID, messageID int64, replyMarkup any) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "editMessageReplyMarkup", payload, nil)
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, png []byte, caption string, replyMarkup any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if caption != "" {
		_ = writer.WriteField("caption", caption)
	}
	if replyMarkup != nil {
		markup, _ := json.Marshal(replyMarkup)
		_ = writer.WriteField("reply_markup", string(markup))
	}
	part, err := writer.CreateFormFile("photo", "card.png")
	if err != nil {
		return fmt.Errorf("create photo form: %w", err)
	}
	if _, err := part.Write(png); err != nil {
		return fmt.Errorf("write photo form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close photo form: %w", err)
	}
	return c.do(ctx, "sendPhoto", writer.FormDataContentType(), &body, nil)
}

func (c *Client) EditMessageMedia(ctx context.Context, chatID, messageID int64, png []byte, caption string, replyMarkup any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	_ = writer.WriteField("message_id", strconv.FormatInt(messageID, 10))

	mediaObj := map[string]any{
		"type":  "photo",
		"media": "attach://photo",
	}
	if caption != "" {
		mediaObj["caption"] = caption
	}
	mediaJSON, _ := json.Marshal(mediaObj)
	_ = writer.WriteField("media", string(mediaJSON))

	if replyMarkup != nil {
		markup, _ := json.Marshal(replyMarkup)
		_ = writer.WriteField("reply_markup", string(markup))
	}

	part, err := writer.CreateFormFile("photo", "card.png")
	if err != nil {
		return fmt.Errorf("create media form: %w", err)
	}
	if _, err := part.Write(png); err != nil {
		return fmt.Errorf("write media form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close media form: %w", err)
	}
	return c.do(ctx, "editMessageMedia", writer.FormDataContentType(), &body, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	payload := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		payload["text"] = text
	}
	return c.postJSON(ctx, "answerCallbackQuery", payload, nil)
}

func (c *Client) AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, errorMessage string) error {
	payload := map[string]any{"pre_checkout_query_id": id, "ok": ok}
	if !ok {
		payload["error_message"] = errorMessage
	}
	return c.postJSON(ctx, "answerPreCheckoutQuery", payload, nil)
}

func (c *Client) SendInvoice(ctx context.Context, chatID int64, title, description, payload string, amount int64, replyMarkup any) error {
	body := map[string]any{
		"chat_id":        chatID,
		"title":          title,
		"description":    description,
		"payload":        payload,
		"provider_token": "",
		"currency":       "XTR",
		"prices":         []LabeledPrice{{Label: title, Amount: amount}},
	}
	if replyMarkup != nil {
		body["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "sendInvoice", body, nil)
}

func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var response struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	err := c.postJSON(ctx, "getUpdates", map[string]any{
		"offset":  offset,
		"timeout": 25,
		"allowed_updates": []string{
			"message", "callback_query", "inline_query", "pre_checkout_query",
		},
	}, &response)
	return response.Result, err
}

func (c *Client) postJSON(ctx context.Context, method string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", method, err)
	}
	return c.do(ctx, method, "application/json", bytes.NewReader(data), out)
}

func (c *Client) do(ctx context.Context, method, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, body)
	if err != nil {
		return fmt.Errorf("create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s request: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(data), "message is not modified") {
			return nil
		}
		return fmt.Errorf("telegram %s status %d: %s", method, resp.StatusCode, string(data))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
	}
	return nil
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	return c.postJSON(ctx, "deleteMessage", payload, nil)
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	var response struct {
		OK     bool `json:"ok"`
		Result File `json:"result"`
	}
	err := c.postJSON(ctx, "getFile", map[string]any{"file_id": fileID}, &response)
	if err != nil {
		return File{}, err
	}
	if !response.OK {
		return File{}, fmt.Errorf("getFile returned ok=false")
	}
	return response.Result, nil
}

func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/bot"+c.token+"/"+filePath, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status code %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
