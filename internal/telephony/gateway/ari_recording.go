package gateway

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ariRecordRequest struct {
	Name               string
	Format             string
	MaxDurationSeconds int
	MaxSilenceSeconds  int
	IfExists           string
	Beep               bool
	TerminateOn        string
}

type ariLiveRecording struct {
	Name      string `json:"name,omitempty"`
	Format    string `json:"format,omitempty"`
	State     string `json:"state,omitempty"`
	Cause     string `json:"cause,omitempty"`
	TargetURI string `json:"target_uri,omitempty"`
}

func (c *ariClient) recordBridge(ctx context.Context, bridgeID string, req ariRecordRequest) (ariLiveRecording, error) {
	query := url.Values{}
	query.Set("name", strings.TrimSpace(req.Name))
	query.Set("format", firstNonEmpty(req.Format, defaultRecordingFormat))
	query.Set("ifExists", firstNonEmpty(req.IfExists, "overwrite"))
	query.Set("terminateOn", firstNonEmpty(req.TerminateOn, "none"))
	query.Set("beep", strconv.FormatBool(req.Beep))
	if req.MaxDurationSeconds > 0 {
		query.Set("maxDurationSeconds", strconv.Itoa(req.MaxDurationSeconds))
	}
	if req.MaxSilenceSeconds > 0 {
		query.Set("maxSilenceSeconds", strconv.Itoa(req.MaxSilenceSeconds))
	}
	var out ariLiveRecording
	err := c.do(ctx, http.MethodPost, "/bridges/"+url.PathEscape(bridgeID)+"/record", query, nil, &out)
	return out, err
}

func (c *ariClient) stopLiveRecording(ctx context.Context, recordingName string) error {
	recordingName = strings.TrimSpace(recordingName)
	if recordingName == "" {
		return nil
	}
	err := c.do(ctx, http.MethodPost, "/recordings/live/"+url.PathEscape(recordingName)+"/stop", nil, nil, nil)
	if isARIStatus(err, http.StatusNotFound) || isARIStatus(err, http.StatusConflict) {
		return nil
	}
	return err
}

func (c *ariClient) downloadStoredRecording(ctx context.Context, recordingName string) ([]byte, error) {
	recordingName = strings.TrimSpace(recordingName)
	if recordingName == "" {
		return nil, nil
	}
	endpoint := c.baseURL + "/recordings/stored/" + url.PathEscape(recordingName) + "/file"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.user != "" || c.password != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, ariStatusError{StatusCode: res.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	return data, readErr
}
