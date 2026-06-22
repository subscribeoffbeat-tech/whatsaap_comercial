package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const metaBaseURL = "https://graph.facebook.com/v23.0"

// Sentinel errors for permanent failures (do not retry).
var (
	ErrNotOnWhatsApp = errors.New("number is not on WhatsApp")
	ErrRateLimit     = errors.New("rate limit hit — retry after back-off")
)

// MetaAPIError wraps an error response from the Meta Graph API.
type MetaAPIError struct {
	Code    int
	Type    string
	Message string
}

func (e *MetaAPIError) Error() string {
	return fmt.Sprintf("meta api error %d (%s): %s", e.Code, e.Type, e.Message)
}

// IsPermanent returns true for errors that should not be retried.
// Per CLAUDE.md: 131049 and "invalid number" class errors are permanent.
func IsPermanent(err error) bool {
	var me *MetaAPIError
	if errors.As(err, &me) {
		switch me.Code {
		case 131049, // contact not on WhatsApp
			131026, // message undeliverable
			132000, // template param mismatch
			130472: // template rejected
			return true
		}
	}
	return errors.Is(err, ErrNotOnWhatsApp)
}

// Client wraps the Meta Cloud API v23.x.
type Client struct {
	phoneNumberID string
	wabaID        string
	accessToken   string
	http          *http.Client
}

func NewClient(phoneNumberID, wabaID, accessToken string) *Client {
	return &Client{
		phoneNumberID: phoneNumberID,
		wabaID:        wabaID,
		accessToken:   accessToken,
		http:          &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Send helpers ──────────────────────────────────────────────────────────

// SendText sends a free-form text message. Only valid inside the 24h window.
func (c *Client) SendText(ctx context.Context, to, body string) (waMessageID string, err error) {
	req := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "text",
		"text": map[string]any{
			"preview_url": false,
			"body":        body,
		},
	}
	return c.sendMessage(ctx, req)
}

// TemplateComponent is a component block for a template message.
type TemplateComponent struct {
	Type       string              `json:"type"`                 // header|body|button
	SubType    string              `json:"sub_type,omitempty"`   // for button: url|quick_reply|call_to_action
	Index      *int                `json:"index,omitempty"`      // for button
	Parameters []TemplateParameter `json:"parameters"`
}

// TemplateParameter is one variable substitution in a template.
type TemplateParameter struct {
	Type     string `json:"type"`               // text|currency|date_time|image|document|video
	Text     string `json:"text,omitempty"`
	ImageID  string `json:"image,omitempty"`    // when type=image: {"id": "..."}
	Payload  string `json:"payload,omitempty"`  // for quick_reply button
}

// SendTemplate sends an approved WhatsApp template message.
// Use outside the 24h window (required) or for structured messages inside it.
func (c *Client) SendTemplate(ctx context.Context, to, templateName, langCode string, components []TemplateComponent) (waMessageID string, err error) {
	tmpl := map[string]any{
		"name":     templateName,
		"language": map[string]string{"code": langCode},
	}
	if len(components) > 0 {
		tmpl["components"] = components
	}
	req := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "template",
		"template":          tmpl,
	}
	if b, jerr := json.Marshal(req); jerr == nil {
		log.Printf("meta send payload to=%s template=%s: %s", to, templateName, string(b))
	}
	return c.sendMessage(ctx, req)
}

// sendMessage is the shared POST /{phone_id}/messages call.
func (c *Client) sendMessage(ctx context.Context, body any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal send request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/messages", metaBaseURL, c.phoneNumberID),
		bytes.NewReader(b),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("send message http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("meta send error status=%d body=%s", resp.StatusCode, string(raw))
		return "", c.parseError(raw)
	}

	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse send response: %w", err)
	}
	if len(out.Messages) == 0 {
		return "", fmt.Errorf("no message ID in response")
	}
	return out.Messages[0].ID, nil
}

// ── Media ─────────────────────────────────────────────────────────────────

// GetMediaURL resolves a Meta media ID to a download URL and mime type.
// Meta media URLs expire after ~5 minutes; download immediately.
func (c *Client) GetMediaURL(ctx context.Context, mediaID string) (url, mimeType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/%s", metaBaseURL, mediaID), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("get media url: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", c.parseError(raw)
	}

	var out struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("parse media url response: %w", err)
	}
	return out.URL, out.MimeType, nil
}

// DownloadMedia fetches media bytes from a signed URL returned by GetMediaURL.
func (c *Client) DownloadMedia(ctx context.Context, mediaURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download media: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// UploadMediaStream uploads an io.Reader as media to Meta and returns the media ID.
// Use this when the file is already in memory (e.g. from a multipart form upload).
func (c *Client) UploadMediaStream(ctx context.Context, r io.Reader, filename, mimeType string) (string, error) {
	var buf bytes.Buffer
	mpw := multipart.NewWriter(&buf)
	_ = mpw.WriteField("messaging_product", "whatsapp")
	fw, err := mpw.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, r); err != nil {
		return "", fmt.Errorf("write media to form: %w", err)
	}
	mpw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/media", metaBaseURL, c.phoneNumberID), &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mpw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload media stream: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", c.parseError(raw)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return out.ID, nil
}

// UploadMedia uploads a local file as media to Meta and returns the media ID.
func (c *Client) UploadMedia(ctx context.Context, filePath, mimeType string) (mediaID string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open media file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("messaging_product", "whatsapp")
	fw, _ := mw.CreateFormFile("file", filepath.Base(filePath))
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("write media to form: %w", err)
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/media", metaBaseURL, c.phoneNumberID),
		&buf,
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload media http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", c.parseError(raw)
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return out.ID, nil
}

// ── Template submission ───────────────────────────────────────────────────

// SubmitTemplateRequest is the payload for the Meta create-template API.
type SubmitTemplateRequest struct {
	Name       string           `json:"name"`
	Language   string           `json:"language"`
	Category   string           `json:"category"`   // MARKETING|UTILITY|AUTHENTICATION (uppercase)
	Components []map[string]any `json:"components"`
}

// SubmitTemplate posts a new template to Meta for approval and returns the Meta template ID.
func (c *Client) SubmitTemplate(ctx context.Context, req SubmitTemplateRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/message_templates", metaBaseURL, c.wabaID),
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("submit template http: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", c.parseError(raw)
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}
	return out.ID, nil
}

// ── Error parsing ─────────────────────────────────────────────────────────

func (c *Client) parseError(body []byte) error {
	var errResp struct {
		Error struct {
			Code    int    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("http error (unparseable body): %s", string(body))
	}
	me := &MetaAPIError{
		Code:    errResp.Error.Code,
		Type:    errResp.Error.Type,
		Message: errResp.Error.Message,
	}
	// Surface known permanent errors as typed sentinels.
	if me.Code == 131049 {
		return fmt.Errorf("%w: %s", ErrNotOnWhatsApp, me.Message)
	}
	if me.Code == 130429 || me.Code == 131048 {
		return fmt.Errorf("%w: code %d", ErrRateLimit, me.Code)
	}
	return me
}
