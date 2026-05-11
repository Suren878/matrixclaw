package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

func (c *Client) SaveStorageFile(ctx context.Context, storagePath string, content []byte, title string, tags []string, mimeType string) (localstorage.Entry, error) {
	request := localstorage.NewFileSaveRequest(storagePath, content, title, tags, mimeType)
	return c.storageFile(ctx, http.MethodPost, "/v1/modules/storage/files", request)
}

func (c *Client) SaveTemporaryStorageFile(ctx context.Context, storagePath string, content []byte, title string, tags []string, mimeType string) (localstorage.TempEntry, error) {
	var response localstorage.TempFileResponse
	request := localstorage.NewFileSaveRequest(storagePath, content, title, tags, mimeType)
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/storage/temp", request, &response); err != nil {
		return localstorage.TempEntry{}, err
	}
	return response.File, nil
}

func (c *Client) ListTemporaryStorageFiles(ctx context.Context, limit int) (localstorage.TempListResult, error) {
	path := "/v1/modules/storage/temp"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var response localstorage.TempListResult
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return localstorage.TempListResult{}, err
	}
	return response, nil
}

func (c *Client) PromoteTemporaryStorageFile(ctx context.Context, tempPath string, destPath string) (localstorage.Entry, error) {
	path := "/v1/modules/storage/temp/" + escapedPath(tempPath) + "/promote"
	request := localstorage.TempPromoteRequest{DestPath: strings.TrimSpace(destPath)}
	return c.storageFile(ctx, http.MethodPost, path, request)
}

func (c *Client) DeleteTemporaryStorageFile(ctx context.Context, tempPath string) (localstorage.TempEntry, error) {
	var response localstorage.TempFileResponse
	path := "/v1/modules/storage/temp/" + escapedPath(tempPath)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return localstorage.TempEntry{}, err
	}
	return response.File, nil
}

func (c *Client) CleanupTemporaryStorageFiles(ctx context.Context) (localstorage.CleanupResult, error) {
	var response localstorage.CleanupResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/storage/temp/cleanup", struct{}{}, &response); err != nil {
		return localstorage.CleanupResult{}, err
	}
	return response.Cleanup, nil
}

func (c *Client) UpdateTemporaryStorageSettings(ctx context.Context, autoCleanup *bool, ttlDays int64, maxGB float64) (localstorage.TempSettings, error) {
	var response localstorage.TempSettingsResponse
	request := localstorage.TempSettingsUpdateRequest{
		AutoCleanup: autoCleanup,
		TTLDays:     ttlDays,
		MaxGB:       maxGB,
	}
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/storage/temp/settings", request, &response); err != nil {
		return localstorage.TempSettings{}, err
	}
	return response.Settings, nil
}

func (c *Client) ListStorageFiles(ctx context.Context, filter localstorage.ListFilter) (localstorage.ListResult, error) {
	values := url.Values{}
	if strings.TrimSpace(filter.Prefix) != "" {
		values.Set("prefix", strings.TrimSpace(filter.Prefix))
	}
	if strings.TrimSpace(filter.Query) != "" {
		values.Set("query", strings.TrimSpace(filter.Query))
	}
	if filter.Limit > 0 {
		values.Set("limit", strconv.Itoa(filter.Limit))
	}
	path := "/v1/modules/storage/files"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response localstorage.ListResult
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return localstorage.ListResult{}, err
	}
	return response, nil
}

func (c *Client) ReadStorageFile(ctx context.Context, storagePath string) (localstorage.ReadResult, error) {
	var response localstorage.ReadResult
	path := "/v1/modules/storage/files/" + escapedPath(storagePath)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return localstorage.ReadResult{}, err
	}
	return response, nil
}

func (c *Client) DeleteStorageFile(ctx context.Context, storagePath string) (localstorage.Entry, error) {
	path := "/v1/modules/storage/files/" + escapedPath(storagePath)
	return c.storageFile(ctx, http.MethodDelete, path, nil)
}

func (c *Client) storageFile(ctx context.Context, method string, path string, request any) (localstorage.Entry, error) {
	var response localstorage.FileResponse
	if err := c.doJSON(ctx, method, path, request, &response); err != nil {
		return localstorage.Entry{}, err
	}
	return response.File, nil
}
