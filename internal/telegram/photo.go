package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strconv"
)

// SentPhoto carries back what the caller needs to reuse the upload: the file id
// Telegram assigned, so the same image is never uploaded twice.
type SentPhoto struct {
	MessageID int64
	FileID    string
}

type sendPhotoResponse struct {
	OK     bool    `json:"ok"`
	Result Message `json:"result"`
}

// largestPhotoFileID picks the file id of the largest reported PhotoSize.
// Telegram always returns the sizes smallest-first, but we don't rely on
// ordering: picking the first blindly would hand back a thumbnail id, and
// every later SendPhotoRef using it would deliver a postage stamp.
func largestPhotoFileID(sizes []PhotoSize) (string, bool) {
	best := ""
	bestArea := -1
	for _, size := range sizes {
		area := size.Width * size.Height
		if area > bestArea {
			bestArea = area
			best = size.FileID
		}
	}
	return best, best != ""
}

// SendPhotoBytes uploads image bytes and returns the resulting message id and
// the file id of the largest size, so callers can cache it and switch to
// SendPhotoRef for subsequent sends of the same image.
func (c *Client) SendPhotoBytes(ctx context.Context, chatID int64, data []byte, caption string, replyMarkup any) (SentPhoto, error) {
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
	part, err := writer.CreateFormFile("photo", "photo.png")
	if err != nil {
		return SentPhoto{}, fmt.Errorf("create photo form: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return SentPhoto{}, fmt.Errorf("write photo form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return SentPhoto{}, fmt.Errorf("close photo form: %w", err)
	}

	var response sendPhotoResponse
	if err := c.do(ctx, "sendPhoto", writer.FormDataContentType(), &body, &response); err != nil {
		return SentPhoto{}, err
	}
	fileID, ok := largestPhotoFileID(response.Result.Photo)
	if !ok {
		return SentPhoto{}, errors.New("sendPhoto response carried no photo sizes")
	}
	return SentPhoto{MessageID: response.Result.MessageID, FileID: fileID}, nil
}

// SendPhotoRef sends an already-uploaded photo by its file id, avoiding a
// re-upload of the same bytes.
func (c *Client) SendPhotoRef(ctx context.Context, chatID int64, fileID, caption string, replyMarkup any) (SentPhoto, error) {
	payload := map[string]any{
		"chat_id": chatID,
		"photo":   fileID,
	}
	if caption != "" {
		payload["caption"] = caption
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	var response sendPhotoResponse
	if err := c.postJSON(ctx, "sendPhoto", payload, &response); err != nil {
		return SentPhoto{}, err
	}
	resultFileID, ok := largestPhotoFileID(response.Result.Photo)
	if !ok {
		return SentPhoto{}, errors.New("sendPhoto response carried no photo sizes")
	}
	return SentPhoto{MessageID: response.Result.MessageID, FileID: resultFileID}, nil
}

// EditMessageMediaRef swaps the photo of an existing message by file id,
// without re-uploading anything.
func (c *Client) EditMessageMediaRef(ctx context.Context, chatID, messageID int64, fileID, caption string, replyMarkup any) error {
	media := map[string]any{"type": "photo", "media": fileID}
	if caption != "" {
		media["caption"] = caption
	}
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"media":      media,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	return c.postJSON(ctx, "editMessageMedia", payload, nil)
}
