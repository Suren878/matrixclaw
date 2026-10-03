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
	"github.com/Suren878/matrixclaw/internal/modules"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
	"github.com/Suren878/matrixclaw/internal/modules/voice/realtime"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/skills"
)

// Deps is what the API serves; the daemon fills every field.
type Deps struct {
	Core       *core.Core
	Automation *automation.Service
	Storage    storageStore
	Realtime   *realtime.Manager
	Modules    Modules
	Skills     *skills.Service
	Setup      *setup.Service
	// Reload makes the running daemon follow setup.json.
	Reload   func(context.Context) error
	Restart  func(context.Context, core.AdminRestartRequest) error
	Stop     func(context.Context) error
	APIToken string
}

// Modules are the daemon modules the API serves settings and actions of.
type Modules struct {
	Set      *modules.Set
	TTS, STT *voicemodule.Module
}

type Server struct {
	Deps
	mux        *http.ServeMux
	statusMu   sync.RWMutex
	startedAt  time.Time
	cpuMu      sync.Mutex
	lastCPU    cpuSnapshot
	hasLastCPU bool
}

func New(deps Deps) *Server {
	deps.APIToken = strings.TrimSpace(deps.APIToken)
	server := &Server{
		Deps:      deps,
		mux:       http.NewServeMux(),
		startedAt: time.Now().UTC(),
	}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.requestAuthorized(r) {
			withRole(s.mux).ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="matrixclaw"`)
		writeErrorMessage(w, http.StatusUnauthorized, "unauthorized")
	})
}

func (s *Server) requestAuthorized(r *http.Request) bool {
	if s.APIToken == "" {
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
		return true
	}
	return constantTimeEqual(bearerToken(r.Header.Get("Authorization")), s.APIToken)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func constantTimeEqual(left string, right string) bool {
	if left == "" || len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (s *Server) routes() {
	routes := []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /v1/health", s.handleHealth},
		{"GET /v1/server/status", s.handleServerStatus},
		{"POST /v1/admin/reload", ownerOnly(s.handleAdminReload)},
		{"POST /v1/admin/restart", ownerOnly(s.handleAdminRestart)},
		{"POST /v1/admin/stop", ownerOnly(s.handleAdminStop)},

		{"GET /v1/setup/providers", s.handleSetupProviders},
		{"PATCH /v1/setup/providers/{id}", ownerOnly(s.handleSetupProviderUpdate)},
		{"DELETE /v1/setup/providers/{id}", ownerOnly(s.handleSetupProviderDelete)},
		{"POST /v1/setup/providers/{id}/models", ownerOnly(s.handleSetupProviderModels)},
		{"GET /v1/session-providers", s.handleSessionProviders},
		{"GET /v1/external-agents", s.handleExternalAgents},
		{"PATCH /v1/external-agents/{id}", ownerOnly(s.handleExternalAgentUpdate)},

		{"GET /v1/automation/jobs", s.handleAutomationJobs},
		{"POST /v1/automation/jobs", s.handleAutomationJobCreate},
		{"GET /v1/automation/jobs/{id}", s.handleAutomationJob},
		{"DELETE /v1/automation/jobs/{id}", s.handleAutomationJobDelete},
		{"POST /v1/automation/jobs/{id}/{action}", s.handleAutomationJobAction},

		{"GET /v1/client-deliveries", s.handleClientDeliveries},
		{"POST /v1/client-deliveries/{id}/ack", s.handleClientDeliveryAck},
		{"POST /v1/client-deliveries/{id}/fail", s.handleClientDeliveryFail},

		{"GET /v1/sessions", s.handleSessions},
		{"POST /v1/sessions", s.handleSessionCreate},
		{"GET /v1/sessions/{id}", s.handleSession},
		{"PATCH /v1/sessions/{id}", s.handleSessionRename},
		{"DELETE /v1/sessions/{id}", s.handleSessionDelete},
		{"PATCH /v1/sessions/{id}/llm", s.handleSessionLLMUpdate},
		{"GET /v1/sessions/{id}/models", s.handleSessionLLMModels},
		{"PATCH /v1/sessions/{id}/permissions", ownerOnly(s.handleSessionPermissionsUpdate)},
		{"GET /v1/sessions/{id}/permission-rules", s.handleSessionPermissionRules},
		{"POST /v1/sessions/{id}/permission-rules", s.handlePermissionRuleCreate},
		{"GET /v1/sessions/{id}/context", s.handleSessionContext},
		{"GET /v1/sessions/{id}/usage", s.handleSessionUsage},
		{"GET /v1/sessions/{id}/budget", s.handleSessionBudget},
		{"PUT /v1/sessions/{id}/budget", s.handleSessionBudgetUpdate},
		{"GET /v1/sessions/{id}/todo", s.handleSessionTodo},
		{"DELETE /v1/sessions/{id}/todo", s.handleSessionTodoClear},
		{"GET /v1/sessions/{id}/tasks", s.handleSessionTasks},
		{"POST /v1/sessions/{id}/compact", s.handleSessionCompact},
		{"POST /v1/sessions/{id}/clear", s.handleSessionClear},
		{"POST /v1/sessions/{id}/system-message", s.handleSessionSystemMessage},
		{"GET /v1/bindings/current", s.handleCurrentBinding},
		{"POST /v1/bindings/use", s.handleUseBinding},
		{"GET /v1/snapshot", s.handleSnapshot},
		{"GET /v1/events", s.handleEvents},
		{"GET /v1/messages", s.handleMessages},
		{"POST /v1/messages", s.handleMessageCreate},
		{"GET /v1/search", s.handleSearch},
		{"GET /v1/memory", s.handleMemory},

		{"GET /v1/runs/{id}", s.handleRun},
		{"GET /v1/runs/{id}/steps", s.handleRunSteps},
		{"GET /v1/runs/{id}/progress", s.handleRunProgress},
		{"POST /v1/runs/{id}/cancel", s.handleRunCancel},
		{"GET /v1/tasks/{id}", s.handleTask},
		{"POST /v1/tasks/{id}/cancel", s.handleTaskCancel},
		{"GET /v1/approvals", s.handleApprovals},
		{"POST /v1/approvals/{id}/resolve", s.handleApprovalResolve},
		{"DELETE /v1/permission-rules/{id}", s.handlePermissionRuleDelete},
		{"GET /v1/tools", s.handleTools},
		{"POST /v1/tools/execute", s.handleToolExecute},

		{"GET /v1/modules", s.handleModules},
		{"GET /v1/settings/{module}", s.handleModuleSettings},
		{"POST /v1/settings/{module}", ownerOnly(s.handleModuleSettingsChange)},
		{"GET /v1/modules/storage/files", s.handleStorageFiles},
		{"POST /v1/modules/storage/files", withBodyLimit(storageSaveJSONBodyLimitBytes, s.handleStorageFileCreate)},
		{"GET /v1/modules/storage/files/{path...}", s.handleStorageFile},
		{"DELETE /v1/modules/storage/files/{path...}", s.handleStorageFileDelete},
		{"GET /v1/modules/storage/temp", s.handleStorageTemp},
		{"POST /v1/modules/storage/temp", withBodyLimit(storageSaveJSONBodyLimitBytes, s.handleStorageTempCreate)},
		{"POST /v1/modules/storage/temp/cleanup", s.handleStorageTempCleanup},
		{"PATCH /v1/modules/storage/temp/settings", ownerOnly(s.handleStorageTempSettings)},
		{"GET /v1/modules/storage/temp/{path...}", s.handleStorageTempFile},
		{"DELETE /v1/modules/storage/temp/{path...}", s.handleStorageTempDelete},
		{"POST /v1/modules/storage/temp/{path}/promote", s.handleStorageTempPromote},
		{"POST /v1/modules/voice/tts", s.handleTextToSpeech},
		{"POST /v1/modules/voice/stt", withBodyLimit(voiceAudioJSONBodyLimitBytes, s.handleSpeechToText)},
		{"GET /v1/modules/voice/realtime_voice", s.handleRealtimeVoiceModule},
		{"POST /v1/realtime-voice/sessions", s.handleRealtimeVoiceSessionCreate},
		{"GET /v1/realtime-voice/sessions/{id}", s.handleRealtimeVoiceSession},
		{"DELETE /v1/realtime-voice/sessions/{id}", s.handleRealtimeVoiceSessionClose},
		{"GET /v1/realtime-voice/sessions/{id}/stream", s.handleRealtimeVoiceStream},
		{"GET /v1/modules/mcp", s.handleMCP},
		{"PATCH /v1/modules/mcp", ownerOnly(s.handleMCPUpdate)},
		{"POST /v1/modules/mcp/servers", ownerOnly(s.handleMCPServerCreate)},
		{"PATCH /v1/modules/mcp/{id}/server", ownerOnly(s.handleMCPServerUpdate)},
		{"DELETE /v1/modules/mcp/{id}/server", ownerOnly(s.handleMCPServerDelete)},
		{"GET /v1/modules/skills", s.handleSkills},
		{"POST /v1/modules/skills", ownerOnly(s.handleSkillInstall)},
		{"POST /v1/modules/skills/drafts", ownerOnly(s.handleSkillDraft)},
		{"GET /v1/modules/skills/{id}", s.handleSkill},
		{"PATCH /v1/modules/skills/{id}", ownerOnly(s.handleSkillUpdate)},
		{"DELETE /v1/modules/skills/{id}", ownerOnly(s.handleSkillDelete)},
		{"PUT /v1/modules/skills/{id}/body", ownerOnly(s.handleSkillBody)},
		{"POST /v1/modules/skills/{id}/{action}", ownerOnly(s.handleSkillAction)},
		{"GET /v1/sessions/{id}/skills", s.handleSessionSkills},
		{"POST /v1/sessions/{id}/skills/{skill}", s.handleSessionSkillUse},
		{"DELETE /v1/sessions/{id}/skills/{skill}", s.handleSessionSkillUnload},
	}
	for _, route := range routes {
		s.mux.HandleFunc(route.pattern, route.handler)
	}
}
