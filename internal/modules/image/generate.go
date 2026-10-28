package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
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
	maxInputImages       = 4
)

type generateTool struct {
	module *Module
}

type generateInput struct {
	Prompt string   `json:"prompt"`
	Size   string   `json:"size,omitempty"`
	Images []string `json:"images,omitempty"`
}

// inputImage is an image the call edits or takes as a reference.
type inputImage struct {
	name, mimeType string
	data           []byte
}

func (t *generateTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          GenerateToolID,
		Description: "Generate an image from a text description, or edit existing images: pass their storage paths in images (a photo the user attached, an image generated earlier). The result is saved to MatrixClaw temporary storage, and in a Telegram chat it is also sent to the chat as a photo.",
		Effect:      tools.EffectMutation,
		Asks:        true,
		Namespace:   "module.image",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "description": "What the image shows: subject, style, composition, lighting, any text it contains. With images: what to change and what to keep."},
    "size": {"type": "string", "description": "Optional WIDTHxHEIGHT, such as 1024x1024 or 1536x1024; omit for the configured size."},
    "images": {"type": "array", "items": {"type": "string"}, "maxItems": 4, "description": "Optional storage paths (temp_path or storage_path) of images to edit or use as references, the first being the one edited."}
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
	action := "Generate an image"
	if len(input.Images) > 0 {
		action = fmt.Sprintf("Edit %d image(s)", len(input.Images))
	}
	return tools.ApprovalRequest{
		Description: fmt.Sprintf("%s with %s on %s.", action, endpoint.model, endpoint.providerName),
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
	images, err := t.readImages(input.Images)
	if err != nil {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	var data []byte
	if len(images) > 0 {
		data, err = edit(ctx, endpoint, input, images)
	} else {
		data, err = generate(ctx, endpoint, input)
	}
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

// readImages reads each path from temporary storage, else from saved storage.
func (t *generateTool) readImages(paths []string) ([]inputImage, error) {
	images := make([]inputImage, 0, len(paths))
	for _, raw := range paths {
		var name, mimeType string
		temp, data, err := t.module.store.ReadTemporaryBytes(raw)
		if err == nil {
			name, mimeType = temp.Path, temp.MIMEType
		} else {
			saved, savedData, savedErr := t.module.store.ReadBytes(raw, maxImageBytes)
			if savedErr != nil {
				return nil, fmt.Errorf("image %s is not in storage: %w", raw, errors.Join(err, savedErr))
			}
			name, mimeType, data = saved.Path, saved.MIMEType, savedData
		}
		if sniffed := http.DetectContentType(data); strings.HasPrefix(sniffed, "image/") {
			mimeType = sniffed
		}
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, fmt.Errorf("%s is %s, not an image", raw, mimeType)
		}
		images = append(images, inputImage{name: path.Base(name), mimeType: mimeType, data: data})
	}
	return images, nil
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
	input.Images = textutil.NonBlank(input.Images...)
	switch {
	case input.Prompt == "":
		return generateInput{}, fmt.Errorf("%w: prompt is required", tools.ErrInvalidArgs)
	case input.Size != "" && input.Size != "auto" && !sizePattern.MatchString(input.Size):
		return generateInput{}, fmt.Errorf("%w: size is WIDTHxHEIGHT, such as 1024x1024", tools.ErrInvalidArgs)
	case len(input.Images) > maxInputImages:
		return generateInput{}, fmt.Errorf("%w: at most %d images", tools.ErrInvalidArgs, maxInputImages)
	}
	return input, nil
}

// generate asks the generations endpoint for one image.
func generate(ctx context.Context, endpoint endpoint, input generateInput) ([]byte, error) {
	request := map[string]any{"model": endpoint.model, "prompt": input.Prompt, "n": 1}
	if size := textutil.FirstNonEmpty(input.Size, endpoint.size); size != "" {
		request["size"] = size
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return send(ctx, endpoint, "/images/generations", "application/json", body)
}

// edit sends the images to the edits endpoint as a multipart form, one
// "image" field, or "image[]" fields for several.
func edit(ctx context.Context, endpoint endpoint, input generateInput, images []inputImage) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := map[string]string{"model": endpoint.model, "prompt": input.Prompt, "n": "1"}
	if size := textutil.FirstNonEmpty(input.Size, endpoint.size); size != "" {
		fields["size"] = size
	}
	for key, value := range fields {
		if err := form.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	field := "image"
	if len(images) > 1 {
		field = "image[]"
	}
	for _, image := range images {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, image.name))
		header.Set("Content-Type", image.mimeType)
		part, err := form.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(image.data); err != nil {
			return nil, err
		}
	}
	if err := form.Close(); err != nil {
		return nil, err
	}
	data, err := send(ctx, endpoint, "/images/edits", form.FormDataContentType(), body.Bytes())
	if errors.Is(err, errNoEndpoint) {
		return nil, fmt.Errorf("%s does not edit images: it has no %s/images/edits endpoint", endpoint.providerName, endpoint.base)
	}
	return data, err
}

var errNoEndpoint = errors.New("endpoint not found")

// send posts to the images API and returns the first image, whether the
// provider answers with base64 or with a URL to download.
func send(ctx context.Context, endpoint endpoint, path, contentType string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
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
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &answer)
	if resp.StatusCode/100 != 2 {
		message := textutil.Clip(strings.TrimSpace(string(raw)), 300, "…")
		if answer.Error != nil && answer.Error.Message != "" {
			message = answer.Error.Message
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s: %s", errNoEndpoint, resp.Status, message)
		}
		return nil, fmt.Errorf("%s: %s", resp.Status, message)
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
