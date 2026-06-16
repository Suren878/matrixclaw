package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
)

type Server struct {
	cfg    Config
	ari    *ariClient
	events *ariEventHub
	mu     sync.RWMutex
	calls  map[string]*Call
	client *http.Client
}

func Run(ctx context.Context, cfg Config) error {
	s := NewServer(cfg)
	if cfg.ARIPassword != "" {
		s.events.Start(ctx)
		safego.Go("telephony.cleanupStaleARIOnReady", func() { s.cleanupStaleARIOnReady(ctx) })
	}
	if cfg.InboundEnabled {
		safego.Go("telephony.inboundListener", func() { s.runInboundListener(ctx) })
	}
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	safego.Go("telephony.httpServer", func() {
		var err error
		if !safego.Run("telephony.httpServer.listen", func() {
			log.Printf("matrixclaw telephony gateway listening on %s", cfg.HTTPAddr)
			err = httpServer.ListenAndServe()
			if err == http.ErrServerClosed {
				err = nil
			}
		}) {
			err = fmt.Errorf("telephony http server panicked")
		}
		errCh <- err
	})
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func NewServer(cfg Config) *Server {
	ari := newARIClient(cfg.ARIURL, cfg.ARIUser, cfg.ARIPassword)
	return &Server{
		cfg:    cfg,
		ari:    ari,
		events: newARIEventHub(ari, cfg.ARIApp),
		calls:  map[string]*Call{},
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Server) probeMatrixclaw(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.MatrixclawURL+"/v1/modules/voice/realtime_voice", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.MatrixclawToken)
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var payload struct {
		Module struct {
			Enabled bool   `json:"enabled"`
			Status  string `json:"status"`
		} `json:"module"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return err
	}
	if !payload.Module.Enabled {
		return errors.New("realtime voice disabled")
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Module.Status), "Ready") {
		return fmt.Errorf("realtime voice status is %s", payload.Module.Status)
	}
	return nil
}
