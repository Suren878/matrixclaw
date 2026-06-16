package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const telephonyGatewayHealthTimeout = 2 * time.Second

func (s *Server) handleTelephonyModule(w http.ResponseWriter, r *http.Request) {
	if s.setup == nil {
		writeErrorMessage(w, http.StatusNotImplemented, "setup service is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getTelephonyModule(w, r)
	case http.MethodPatch:
		s.updateTelephonyModule(w, r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
	}
}

func (s *Server) getTelephonyModule(w http.ResponseWriter, r *http.Request) {
	module, err := s.setup.TelephonyModule()
	if err != nil {
		writeErrorMessage(w, http.StatusInternalServerError, err.Error())
		return
	}
	module = decorateTelephonyModule(r.Context(), module, s.setup)
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: module})
}

func (s *Server) updateTelephonyModule(w http.ResponseWriter, r *http.Request) {
	var update setup.TelephonyModuleUpdate
	if !decodeJSONBody(w, r, &update) {
		return
	}
	module, err := s.setup.UpdateTelephonyModule(update)
	if err != nil {
		writeErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.adminReload != nil {
		if err := s.adminReload(r.Context()); err != nil {
			writeErrorMessage(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	module = decorateTelephonyModule(r.Context(), module, s.setup)
	writeJSON(w, http.StatusOK, setup.TelephonyModuleResponse{Module: module})
}

func decorateTelephonyModule(ctx context.Context, module setup.TelephonyModuleDescriptor, service *setup.Service) setup.TelephonyModuleDescriptor {
	if strings.TrimSpace(module.GatewayURL) == "" {
		return module
	}
	cfg, err := service.Load()
	if err != nil {
		module.GatewayError = err.Error()
		return module
	}
	token := strings.TrimSpace(cfg.Modules.Telephony.GatewayToken)
	if err := probeTelephonyGateway(ctx, module.GatewayURL, token); err != nil {
		module.GatewayReachable = false
		module.GatewayError = err.Error()
		if strings.HasPrefix(err.Error(), "gateway status is ") {
			module.GatewayReachable = true
			if module.Enabled {
				module.Status = "Gateway degraded"
			}
			return module
		}
		if module.Enabled {
			module.Status = "Gateway unreachable"
		}
		return module
	}
	module.GatewayReachable = true
	if module.Enabled {
		module.Status = "Ready"
	}
	return module
}

func probeTelephonyGateway(ctx context.Context, gatewayURL string, token string) error {
	gatewayURL = strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	if gatewayURL == "" {
		return fmt.Errorf("gateway URL is required")
	}
	probeCtx, cancel := context.WithTimeout(ctx, telephonyGatewayHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, gatewayURL+"/v1/health", nil)
	if err != nil {
		return err
	}
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("gateway health returned HTTP %d", res.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err == nil {
		if !strings.EqualFold(strings.TrimSpace(payload.Status), "ready") {
			return fmt.Errorf("gateway status is %s", firstNonEmpty(strings.TrimSpace(payload.Status), strings.TrimSpace(payload.Error), "not ready"))
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
