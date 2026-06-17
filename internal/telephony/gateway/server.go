package gateway

import (
	"context"
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
	cfg     Config
	ari     *ariClient
	events  *ariEventHub
	rootMu  sync.RWMutex
	rootCtx context.Context
	cancel  context.CancelFunc
	mu      sync.RWMutex
	calls   map[string]*Call
	api     *matrixclawClient
}

func Run(ctx context.Context, cfg Config) error {
	s := NewServer(cfg)
	s.bindRootContext(ctx)
	defer s.shutdownActiveCalls()
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
		s.shutdownActiveCalls()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func NewServer(cfg Config) *Server {
	ari := newARIClient(cfg.ARIURL, cfg.ARIUser, cfg.ARIPassword)
	rootCtx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:     cfg,
		ari:     ari,
		events:  newARIEventHub(ari, cfg.ARIApp),
		rootCtx: rootCtx,
		cancel:  cancel,
		calls:   map[string]*Call{},
		api:     newMatrixclawClient(cfg.MatrixclawURL, cfg.MatrixclawToken, &http.Client{Timeout: 45 * time.Second}),
	}
}

func (s *Server) bindRootContext(parent context.Context) {
	if s == nil || parent == nil {
		return
	}
	rootCtx, cancel := context.WithCancel(parent)
	s.rootMu.Lock()
	previousCancel := s.cancel
	s.rootCtx = rootCtx
	s.cancel = cancel
	s.rootMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
}

func (s *Server) callContext() (context.Context, context.CancelFunc) {
	if s == nil {
		return context.WithCancel(context.Background())
	}
	s.rootMu.RLock()
	root := s.rootCtx
	s.rootMu.RUnlock()
	if root == nil {
		root = context.Background()
	}
	if s.cfg.MaxCallDuration > 0 {
		return context.WithTimeout(root, s.cfg.MaxCallDuration)
	}
	return context.WithCancel(root)
}

func (s *Server) shutdownActiveCalls() {
	if s == nil {
		return
	}
	s.rootMu.RLock()
	cancel := s.cancel
	s.rootMu.RUnlock()
	if cancel != nil {
		cancel()
	}
	for _, call := range s.callList() {
		cancelCall(call)
	}
}

func (s *Server) probeMatrixclaw(ctx context.Context) error {
	var payload struct {
		Module struct {
			Enabled bool   `json:"enabled"`
			Status  string `json:"status"`
		} `json:"module"`
	}
	if err := s.api.getJSON(ctx, "/v1/modules/voice/realtime_voice", &payload); err != nil {
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
