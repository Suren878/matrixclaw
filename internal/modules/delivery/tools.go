package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	sendFileToolID         = "send_file"
	maxSendFileBytes int64 = 50 * 1024 * 1024
)

type storageReader interface {
	ReadBytes(rawPath string, maxBytes int64) (localstorage.Entry, []byte, error)
	ReadTemporaryBytes(rawPath string) (localstorage.TempEntry, []byte, error)
}

type deliveryCreator interface {
	CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error)
}

type sendFileTool struct {
	store      storageReader
	deliveries deliveryCreator
}

type sendFileInput struct {
	Path      string `json:"path"`
	Temporary bool   `json:"temporary"`
	FileName  string `json:"file_name"`
	Caption   string `json:"caption"`
	MIMEType  string `json:"mime_type"`
}

type sendFileSource struct {
	Path     string
	Title    string
	MIMEType string
	Size     int64
}

func NewSendFileTool(store storageReader, deliveries deliveryCreator) tools.Executor {
	return &sendFileTool{store: store, deliveries: deliveries}
}

func (t *sendFileTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          sendFileToolID,
		Description: "Send a MatrixClaw storage file to the current Telegram chat as a document. Use when the user asks you to send, attach, or share a saved file. The file must already be in MatrixClaw storage; use storage_list, storage_save, or storage_save_temp first if needed.",
		Effect:      tools.EffectMutation,
		Asks:        true,
		Namespace:   "module.delivery",
		Category:    tools.CategoryStorage,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "MatrixClaw storage path to send"},
    "temporary": {"type": "boolean", "description": "Set true for a temporary storage path"},
    "file_name": {"type": "string", "description": "Optional Telegram document file name override"},
    "caption": {"type": "string", "description": "Optional Telegram document caption"},
    "mime_type": {"type": "string", "description": "Optional MIME type override"}
  },
  "required": ["path"],
  "additionalProperties": false
}`),
	}
}

// document reads a send_file call into the document it delivers to the
// call's Telegram chat; the file must be in storage.
func (t *sendFileTool) document(call tools.Call) (core.DocumentDeliveryPayload, error) {
	if t == nil || t.store == nil || t.deliveries == nil {
		return core.DocumentDeliveryPayload{}, errors.New("file delivery is not configured")
	}
	var input sendFileInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return core.DocumentDeliveryPayload{}, errors.New("invalid send_file arguments")
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return core.DocumentDeliveryPayload{}, errors.New("send_file requires path")
	}
	if !strings.EqualFold(strings.TrimSpace(call.Client), "telegram") || strings.TrimSpace(call.ExternalKey) == "" {
		return core.DocumentDeliveryPayload{}, errors.New("send_file is available only from an active Telegram chat")
	}
	source, err := t.readSource(input)
	if err != nil {
		return core.DocumentDeliveryPayload{}, fmt.Errorf("file delivery failed: %w", err)
	}
	return core.DocumentDeliveryPayload{
		StoragePath: source.Path,
		Temporary:   input.Temporary,
		FileName:    deliveryFileName(input.FileName, source),
		Caption:     strings.TrimSpace(input.Caption),
		MIMEType:    firstNonEmpty(strings.TrimSpace(input.MIMEType), source.MIMEType, "application/octet-stream"),
		Size:        source.Size,
	}, nil
}

func (t *sendFileTool) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	payload, err := t.document(call)
	return tools.ApprovalRequest{
		Path:        payload.StoragePath,
		Description: fmt.Sprintf("Send %s (%d bytes) to the current Telegram chat.", payload.FileName, payload.Size),
		Params:      payload,
	}, err
}

func (t *sendFileTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	payload, err := t.document(call)
	if err != nil {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	client, externalKey, fileName := strings.TrimSpace(call.Client), strings.TrimSpace(call.ExternalKey), payload.FileName

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return tools.Result{}, err
	}
	delivery, err := t.deliveries.CreateClientDelivery(ctx, core.ClientDelivery{
		Type:        core.ClientDeliveryTypeDocument,
		Client:      client,
		ExternalKey: externalKey,
		SessionID:   call.SessionID,
		RunID:       call.RunID,
		Summary:     "Send file: " + fileName,
		Payload:     payloadRaw,
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Content:  fmt.Sprintf("Queued file delivery: %s", fileName),
		Metadata: delivery,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *sendFileTool) readSource(input sendFileInput) (sendFileSource, error) {
	if input.Temporary {
		entry, data, err := t.store.ReadTemporaryBytes(input.Path)
		if err != nil {
			return sendFileSource{}, err
		}
		if int64(len(data)) > maxSendFileBytes {
			return sendFileSource{}, fmt.Errorf("file is too large: %d bytes exceeds %d", len(data), maxSendFileBytes)
		}
		return sendFileSource{Path: entry.Path, Title: entry.Title, MIMEType: entry.MIMEType, Size: entry.Size}, nil
	}
	entry, data, err := t.store.ReadBytes(input.Path, maxSendFileBytes)
	if err != nil {
		return sendFileSource{}, err
	}
	if int64(len(data)) > maxSendFileBytes {
		return sendFileSource{}, fmt.Errorf("file is too large: %d bytes exceeds %d", len(data), maxSendFileBytes)
	}
	return sendFileSource{Path: entry.Path, Title: entry.Title, MIMEType: entry.MIMEType, Size: entry.Size}, nil
}

func deliveryFileName(override string, source sendFileSource) string {
	name := firstNonEmpty(strings.TrimSpace(override), source.Title, filepath.Base(source.Path))
	if name == "." || name == string(filepath.Separator) {
		return "matrixclaw-file"
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
