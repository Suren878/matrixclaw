package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/ids"
	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	GenerateToolID = "generate_image"
	// Local models take a minute or more for a large image.
	requestTimeout       = 5 * time.Minute
	maxImageBytes  int64 = 50 << 20
)

type generateTool struct {
	module *Module
}

type generateInput struct {
	Prompt string `json:"prompt"`
	Size   string `json:"size"`
}

func (t *generateTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          GenerateToolID,
		Description: "Generate an image from a text description. The image is saved to MatrixClaw temporary storage, and in a Telegram chat it is also sent to the chat as a photo.",
		Effect:      tools.EffectMutation,
		Asks:        true,
		Namespace:   "module.image",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "What the image shows: subject, style, composition, lighting, any text it contains."},
    "size": {"type": "string", "description": "Optional WIDTHxHEIGHT, such as 1024x1024 or 1536x1024; omit for the configured size."}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`),
	}
}

func (t *generateTool) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	input, err := readInput(call)
	if err != nil {
		return tools.ApprovalRequest{}, err
	}
	endpoint, err := t.module.endpoint()
	if err != nil {
		return tools.ApprovalRequest{}, err
	}
	return tools.ApprovalRequest{
		Description: fmt.Sprintf("Generate an image with %s on %s.", endpoint.model, endpoint.providerName),
		Params:      input,
	}, nil
}

func (t *generateTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	input, err := readInput(call)
	if err != nil {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	endpoint, err := t.module.endpoint()
	if err != nil {
		return tools.Result{Content: "Image generation is unavailable: " + err.Error(), Status: tools.ResultStatusError}, nil
	}
	data, err := generate(ctx, endpoint, input)
	if err != nil {
		return tools.Result{Content: "Image generation failed: " + err.Error(), Status: tools.ResultStatusError}, nil
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return tools.Result{Content: "Image generation failed: the provider returned " + mimeType + ", not an image", Status: tools.ResultStatusError}, nil
	}
	path := "images/" + time.Now().UTC().Format("20060102-150405") + "-" + ids.New("img") + extension(mimeType)
	entry, err := t.module.store.SaveTemporary(path, data, textutil.Clip(input.Prompt, 120, "…"), []string{"generated", "image"}, mimeType)
	if err != nil {
		return tools.Result{}, fmt.Errorf("save generated image: %w", err)
	}
	content := fmt.Sprintf("Image generated (%d KB) and saved to temporary storage at %s.", entry.Size/1024, entry.Path)
	metadata := map[string]any{"path": entry.Path, "temporary": true, "mime_type": mimeType, "size": entry.Size, "model": endpoint.model}
	if client := strings.TrimSpace(call.Client); strings.EqualFold(client, "telegram") && strings.TrimSpace(call.ExternalKey) != "" {
		delivery, err := t.deliver(ctx, call, entry.Path, mimeType, entry.Size)
		if err != nil {
			return tools.Result{}, err
		}
		content += " It was sent to the Telegram chat as a photo."
		metadata["delivery_id"] = delivery.ID
	}
	return tools.Result{Content: content, Metadata: metadata, MIMEType: mimeType, Status: tools.ResultStatusSuccess}, nil
}

func (t *generateTool) deliver(ctx context.Context, call tools.Call, path, mimeType string, size int64) (core.ClientDelivery, error) {
	payload, err := json.Marshal(core.DocumentDeliveryPayload{
		StoragePath: path,
		Temporary:   true,
		FileName:    path[strings.LastIndex(path, "/")+1:],
		MIMEType:    mimeType,
		Size:        size,
		Photo:       true,
	})
	if err != nil {
		return core.ClientDelivery{}, err
	}
	return t.module.deliveries.CreateClientDelivery(ctx, core.ClientDelivery{
		Type:        core.ClientDeliveryTypeDocument,
		Client:      strings.TrimSpace(call.Client),
		ExternalKey: strings.TrimSpace(call.ExternalKey),
		SessionID:   call.SessionID,
		RunID:       call.RunID,
		Summary:     "Generated image",
		Payload:     payload,
	})
}

func readInput(call tools.Call) (generateInput, error) {
	var input generateInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return generateInput{}, fmt.Errorf("%w: %w", tools.ErrInvalidArgs, err)
	}
	input.Prompt, input.Size = strings.TrimSpace(input.Prompt), strings.TrimSpace(input.Size)
	if input.Prompt == "" {
		return generateInput{}, fmt.Errorf("%w: prompt is required", tools.ErrInvalidArgs)
	}
	if input.Size != "" && input.Size != "auto" && !sizePattern.MatchString(input.Size) {
		return generateInput{}, fmt.Errorf("%w: size is WIDTHxHEIGHT, such as 1024x1024", tools.ErrInvalidArgs)
	}
	return input, nil
}

// generate asks for one image and returns its bytes, whether the provider
// answers with base64 or with a URL to download.
func generate(ctx context.Context, endpoint endpoint, input generateInput) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request := map[string]any{"model": endpoint.model, "prompt": input.Prompt, "n": 1}
	if size := textutil.FirstNonEmpty(input.Size, endpoint.size); size != "" {
		request["size"] = size
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if endpoint.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes*2))
	if err != nil {
		return nil, err
	}
	var answer struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &answer)
	if resp.StatusCode/100 != 2 {
		if answer.Error != nil && answer.Error.Message != "" {
			return nil, fmt.Errorf("%s: %s", resp.Status, answer.Error.Message)
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, textutil.Clip(strings.TrimSpace(string(raw)), 300, "…"))
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("unreadable answer: %w", decodeErr)
	}
	if len(answer.Data) == 0 {
		return nil, errors.New("the answer holds no image")
	}
	if image := strings.TrimSpace(answer.Data[0].B64JSON); image != "" {
		return base64.StdEncoding.DecodeString(image)
	}
	if url := strings.TrimSpace(answer.Data[0].URL); url != "" {
		return download(ctx, url)
	}
	return nil, errors.New("the answer holds no image")
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxImageBytes {
		return nil, fmt.Errorf("image is larger than %d bytes", maxImageBytes)
	}
	return data, nil
}

func extension(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
