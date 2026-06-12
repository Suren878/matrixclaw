package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
)

type Server struct {
	core          *core.Core
	automation    *automation.Service
	storage       storageStore
	realtimeVoice *realtime.Manager
	skills        *skills.Service
	mux           *http.ServeMux
	setup         *setup.Service
	adminReload   func(context.Context) error
	adminRestart  func(context.Context, core.AdminRestartRequest) error
	adminStop     func(context.Context) error
	apiToken      string
	statusMu      sync.RWMutex
	startedAt     time.Time
	cpuMu         sync.Mutex
	lastCPU       cpuSnapshot
	hasLastCPU    bool
}

func New(coreService *core.Core) *Server {
	server := &Server{
		core:      coreService,
		mux:       http.NewServeMux(),
		startedAt: time.Now().UTC(),
	}
	server.routes()
	return server
}

func (s *Server) SetAdminReload(fn func(context.Context) error) {
	s.adminReload = fn
}

func (s *Server) SetAdminRestart(fn func(context.Context, core.AdminRestartRequest) error) {
	s.adminRestart = fn
}

func (s *Server) SetAdminStop(fn func(context.Context) error) {
	s.adminStop = fn
}

func (s *Server) SetSetupService(service *setup.Service) {
	s.setup = service
}

func (s *Server) SetAutomationService(service *automation.Service) {
	s.automation = service
}

func (s *Server) SetStorageStore(store storageStore) {
	s.storage = store
}

func (s *Server) SetRealtimeVoiceService(service *realtime.Manager) {
	s.realtimeVoice = service
}

func (s *Server) SetSkillsService(service *skills.Service) {
	s.skills = service
}

func (s *Server) SetAPIToken(token string) {
	s.apiToken = strings.TrimSpace(token)
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.requestAuthorized(r) {
			s.mux.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="matrixclaw"`)
		writeErrorMessage(w, http.StatusUnauthorized, "unauthorized")
	})
}

func (s *Server) requestAuthorized(r *http.Request) bool {
	token := strings.TrimSpace(s.apiToken)
	if token == "" {
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
		return true
	}
	presented := bearerToken(r.Header.Get("Authorization"))
	return constantTimeEqual(presented, token)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func constantTimeEqual(left string, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (s *Server) routes() {
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/server/status", s.handleServerStatus)
	s.mux.HandleFunc("/v1/setup/providers", s.handleSetupProviders)
	s.mux.HandleFunc("/v1/setup/providers/", s.handleSetupProviderByID)
	s.mux.HandleFunc("/v1/session-providers", s.handleSessionProviders)
	s.mux.HandleFunc("/v1/external-agents", s.handleExternalAgents)
	s.mux.HandleFunc("/v1/external-agents/", s.handleExternalAgentByID)
	s.mux.HandleFunc("/v1/admin/reload", s.handleAdminReload)
	s.mux.HandleFunc("/v1/admin/restart", s.handleAdminRestart)
	s.mux.HandleFunc("/v1/admin/stop", s.handleAdminStop)
	s.mux.HandleFunc("/v1/automation/jobs", s.handleAutomationJobs)
	s.mux.HandleFunc("/v1/automation/jobs/", s.handleAutomationJobByID)
	s.mux.HandleFunc("/v1/client-deliveries", s.handleClientDeliveries)
	s.mux.HandleFunc("/v1/client-deliveries/", s.handleClientDeliveryByID)
	s.mux.HandleFunc("/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/v1/sessions/", s.handleSessionByID)
	s.mux.HandleFunc("/v1/bindings/current", s.handleCurrentBinding)
	s.mux.HandleFunc("/v1/bindings/use", s.handleUseBinding)
	s.mux.HandleFunc("/v1/tools", s.handleTools)
	s.mux.HandleFunc("/v1/tools/execute", s.handleToolExecute)
	s.mux.HandleFunc("/v1/modules/storage/files", s.handleStorageFiles)
	s.mux.HandleFunc("/v1/modules/storage/files/", s.handleStorageFileByPath)
	s.mux.HandleFunc("/v1/modules/storage/temp", s.handleStorageTemp)
	s.mux.HandleFunc("/v1/modules/storage/temp/", s.handleStorageTempByPath)
	s.mux.HandleFunc("/v1/modules/voice", s.handleVoiceModules)
	s.mux.HandleFunc("/v1/modules/voice/realtime_voice", s.handleRealtimeVoiceModule)
	s.mux.HandleFunc("/v1/modules/voice/", s.handleVoiceModuleByID)
	s.mux.HandleFunc("/v1/realtime-voice/sessions", s.handleRealtimeVoiceSessions)
	s.mux.HandleFunc("/v1/realtime-voice/sessions/", s.handleRealtimeVoiceSessionByID)
	s.mux.HandleFunc("/v1/modules/web-search", s.handleWebSearch)
	s.mux.HandleFunc("/v1/modules/browser", s.handleBrowserModule)
	s.mux.HandleFunc("/v1/modules/browser/providers/", s.handleBrowserProvider)
	s.mux.HandleFunc("/v1/modules/mcp", s.handleMCP)
	s.mux.HandleFunc("/v1/modules/mcp/", s.handleMCPByID)
	s.mux.HandleFunc("/v1/modules/skills", s.handleSkills)
	s.mux.HandleFunc("/v1/modules/skills/", s.handleSkillByID)
	s.mux.HandleFunc("/v1/approvals", s.handleApprovals)
	s.mux.HandleFunc("/v1/approvals/", s.handleApprovalByID)
	s.mux.HandleFunc("/v1/events", s.handleEvents)
	s.mux.HandleFunc("/v1/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("/v1/search", s.handleSearch)
	s.mux.HandleFunc("/v1/memory", s.handleMemory)
	s.mux.HandleFunc("/v1/messages", s.handleMessages)
	s.mux.HandleFunc("/v1/runs/", s.handleRunByID)
}
