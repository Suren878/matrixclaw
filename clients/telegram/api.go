package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

type BotAPI interface {
	GetMe(ctx context.Context) (User, error)
	GetUpdates(ctx context.Context, req GetUpdatesRequest) ([]Update, error)
	GetFile(ctx context.Context, fileID string) (File, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
	SendMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error)
	SendMessageDraft(ctx context.Context, req SendMessageDraftRequest) error
	SendChatAction(ctx context.Context, req SendChatActionRequest) error
	SendVoice(ctx context.Context, req SendVoiceRequest) (SentMessage, error)
	SendAudio(ctx context.Context, req SendAudioRequest) (SentMessage, error)
	SendDocument(ctx context.Context, req SendDocumentRequest) (SentMessage, error)
	EditMessageText(ctx context.Context, req EditMessageTextRequest) (EditMessageTextResponse, error)
	EditMessageMedia(ctx context.Context, req EditMessageMediaRequest) (EditMessageMediaResponse, error)
	AnswerCallbackQuery(ctx context.Context, req AnswerCallbackQueryRequest) error
	AnswerGuestQuery(ctx context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error)
	AnswerInlineQuery(ctx context.Context, req AnswerInlineQueryRequest) error
	DeleteMessage(ctx context.Context, req DeleteMessageRequest) error
	SetMyCommands(ctx context.Context, req SetMyCommandsRequest) error
	DeleteMyCommands(ctx context.Context, req DeleteMyCommandsRequest) error
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientConfig struct {
	Token      string
	BaseURL    string
	HTTPClient HTTPDoer
}

type Client struct {
	baseURL string
	token   string
	http    HTTPDoer
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type APIError struct {
	Method      string
	StatusCode  int
	ErrorCode   int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{fmt.Sprintf("telegram api %s failed", e.Method)}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("http %d", e.StatusCode))
	}
	if e.ErrorCode != 0 {
		parts = append(parts, fmt.Sprintf("api %d", e.ErrorCode))
	}
	if e.Description != "" {
		parts = append(parts, e.Description)
	}
	return strings.Join(parts, ": ")
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("telegram: token is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultTelegramBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    cfg.HTTPClient,
	}, nil
}

func (c *Client) GetUpdates(ctx context.Context, req GetUpdatesRequest) ([]Update, error) {
	result, err := callAPI[[]Update](ctx, c, "getUpdates", req)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	return callAPI[User](ctx, c, "getMe", struct{}{})
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	return callAPI[File](ctx, c, "getFile", GetFileRequest{FileID: strings.TrimSpace(fileID)})
}

func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	filePath = strings.TrimLeft(strings.TrimSpace(filePath), "/")
	if filePath == "" {
		return nil, fmt.Errorf("telegram: file path is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/bot"+c.token+"/"+filePath, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram: build file download request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("telegram: execute file download request: %w", redactBotTokenError(err, c.token))
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("telegram: read file download response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeAPIError("downloadFile", response.StatusCode, content)
	}
	return content, nil
}

func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (SentMessage, error) {
	return callAPI[SentMessage](ctx, c, "sendMessage", req)
}

func (c *Client) SendMessageDraft(ctx context.Context, req SendMessageDraftRequest) error {
	_, err := callAPI[bool](ctx, c, "sendMessageDraft", req)
	return err
}

func (c *Client) SendChatAction(ctx context.Context, req SendChatActionRequest) error {
	_, err := callAPI[bool](ctx, c, "sendChatAction", req)
	return err
}

func (c *Client) SendVoice(ctx context.Context, req SendVoiceRequest) (SentMessage, error) {
	fields := map[string]string{
		"chat_id": strconv.FormatInt(req.ChatID, 10),
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		fields["caption"] = caption
	}
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "voice.mp3"
	}
	return callMultipartAPI[SentMessage](ctx, c, "sendVoice", fields, "voice", fileName, req.MIMEType, req.Voice)
}

func (c *Client) SendAudio(ctx context.Context, req SendAudioRequest) (SentMessage, error) {
	fields := map[string]string{
		"chat_id": strconv.FormatInt(req.ChatID, 10),
	}
	if req.DisableNotification {
		fields["disable_notification"] = "true"
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		fields["caption"] = caption
	}
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "audio.wav"
	}
	return callMultipartAPI[SentMessage](ctx, c, "sendAudio", fields, "audio", fileName, req.MIMEType, req.Audio)
}

func (c *Client) SendDocument(ctx context.Context, req SendDocumentRequest) (SentMessage, error) {
	fields := map[string]string{
		"chat_id": strconv.FormatInt(req.ChatID, 10),
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		fields["caption"] = caption
	}
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "document"
	}
	return callMultipartAPI[SentMessage](ctx, c, "sendDocument", fields, "document", fileName, req.MIMEType, req.Document)
}

func (c *Client) EditMessageText(ctx context.Context, req EditMessageTextRequest) (EditMessageTextResponse, error) {
	result, err := callAPI[editMessageTextResult](ctx, c, "editMessageText", req)
	return result.EditMessageTextResponse, err
}

type editMessageTextResult struct {
	EditMessageTextResponse
}

func (r *editMessageTextResult) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "true" {
		*r = editMessageTextResult{}
		return nil
	}
	var response EditMessageTextResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}
	r.EditMessageTextResponse = response
	return nil
}

func (c *Client) EditMessageMedia(ctx context.Context, req EditMessageMediaRequest) (EditMessageMediaResponse, error) {
	result, err := callAPI[editMessageMediaResult](ctx, c, "editMessageMedia", req)
	return result.EditMessageMediaResponse, err
}

type editMessageMediaResult struct {
	EditMessageMediaResponse
}

func (r *editMessageMediaResult) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "true" {
		*r = editMessageMediaResult{}
		return nil
	}
	var response EditMessageMediaResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}
	r.EditMessageMediaResponse = response
	return nil
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, req AnswerCallbackQueryRequest) error {
	_, err := callAPI[bool](ctx, c, "answerCallbackQuery", req)
	return err
}

func (c *Client) AnswerGuestQuery(ctx context.Context, req AnswerGuestQueryRequest) (SentGuestMessage, error) {
	return callAPI[SentGuestMessage](ctx, c, "answerGuestQuery", req)
}

func (c *Client) AnswerInlineQuery(ctx context.Context, req AnswerInlineQueryRequest) error {
	_, err := callAPI[bool](ctx, c, "answerInlineQuery", req)
	return err
}

func (c *Client) DeleteMessage(ctx context.Context, req DeleteMessageRequest) error {
	_, err := callAPI[bool](ctx, c, "deleteMessage", req)
	return err
}

func (c *Client) SetMyCommands(ctx context.Context, req SetMyCommandsRequest) error {
	_, err := callAPI[bool](ctx, c, "setMyCommands", req)
	return err
}

func (c *Client) DeleteMyCommands(ctx context.Context, req DeleteMyCommandsRequest) error {
	_, err := callAPI[bool](ctx, c, "deleteMyCommands", req)
	return err
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if strings.EqualFold(apiErr.Method, "getUpdates") && (apiErr.ErrorCode == http.StatusConflict || apiErr.StatusCode == http.StatusConflict) {
			return true
		}
		return apiErr.ErrorCode == http.StatusTooManyRequests || apiErr.StatusCode == http.StatusTooManyRequests || apiErr.ErrorCode >= 500 || apiErr.StatusCode >= 500
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

func callAPI[T any](ctx context.Context, c *Client, method string, payload any) (T, error) {
	var zero T

	body, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("telegram: marshal %s request: %w", method, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("telegram: build %s request: %w", method, err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return zero, fmt.Errorf("telegram: execute %s request: %w", method, redactBotTokenError(err, c.token))
	}
	defer response.Body.Close()

	content, err := io.ReadAll(response.Body)
	if err != nil {
		return zero, fmt.Errorf("telegram: read %s response: %w", method, err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return zero, decodeAPIError(method, response.StatusCode, content)
	}

	var envelope apiResponse[T]
	if err := json.Unmarshal(content, &envelope); err != nil {
		return zero, fmt.Errorf("telegram: decode %s response: %w", method, err)
	}
	if !envelope.OK {
		return zero, &APIError{
			Method:      method,
			StatusCode:  response.StatusCode,
			ErrorCode:   envelope.ErrorCode,
			Description: envelope.Description,
			RetryAfter:  time.Duration(envelope.Parameters.RetryAfter) * time.Second,
		}
	}

	return envelope.Result, nil
}

func callMultipartAPI[T any](ctx context.Context, c *Client, method string, fields map[string]string, fileField string, fileName string, mimeType string, content []byte) (T, error) {
	var zero T
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return zero, fmt.Errorf("telegram: build %s field %s: %w", method, key, err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return zero, fmt.Errorf("telegram: build %s file field: %w", method, err)
	}
	if _, err := part.Write(content); err != nil {
		return zero, fmt.Errorf("telegram: write %s file field: %w", method, err)
	}
	if err := writer.Close(); err != nil {
		return zero, fmt.Errorf("telegram: close %s multipart body: %w", method, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, body)
	if err != nil {
		return zero, fmt.Errorf("telegram: build %s request: %w", method, err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if strings.TrimSpace(mimeType) != "" {
		request.Header.Set("X-Matrixclaw-Upload-Mime", strings.TrimSpace(mimeType))
	}
	response, err := c.http.Do(request)
	if err != nil {
		return zero, fmt.Errorf("telegram: execute %s request: %w", method, redactBotTokenError(err, c.token))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return zero, fmt.Errorf("telegram: read %s response: %w", method, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return zero, decodeAPIError(method, response.StatusCode, responseBody)
	}
	var envelope apiResponse[T]
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return zero, fmt.Errorf("telegram: decode %s response: %w", method, err)
	}
	if !envelope.OK {
		return zero, &APIError{
			Method:      method,
			StatusCode:  response.StatusCode,
			ErrorCode:   envelope.ErrorCode,
			Description: envelope.Description,
			RetryAfter:  time.Duration(envelope.Parameters.RetryAfter) * time.Second,
		}
	}
	return envelope.Result, nil
}

func redactBotTokenInError(err error, token string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	token = strings.TrimSpace(token)
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, "/bot"+token+"/", "/bot<redacted>/")
}

func redactBotTokenError(err error, token string) error {
	if err == nil {
		return nil
	}
	return redactedBotTokenError{
		message: redactBotTokenInError(err, token),
		cause:   err,
	}
}

type redactedBotTokenError struct {
	message string
	cause   error
}

func (e redactedBotTokenError) Error() string {
	return e.message
}

func (e redactedBotTokenError) Unwrap() error {
	return e.cause
}

func decodeAPIError(method string, statusCode int, content []byte) error {
	var payload apiResponse[json.RawMessage]
	if err := json.Unmarshal(content, &payload); err == nil && (payload.Description != "" || payload.ErrorCode != 0) {
		return &APIError{
			Method:      method,
			StatusCode:  statusCode,
			ErrorCode:   payload.ErrorCode,
			Description: payload.Description,
			RetryAfter:  time.Duration(payload.Parameters.RetryAfter) * time.Second,
		}
	}
	return &APIError{
		Method:      method,
		StatusCode:  statusCode,
		Description: string(bytes.TrimSpace(content)),
	}
}
