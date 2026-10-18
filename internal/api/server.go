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

// route is one entry of the route table: a ServeMux pattern, the policy that
// says who may use it (role.go) and its handler.
type route struct {
	pattern string
	policy  policy
	handler http.HandlerFunc
}

func (s *Server) routes() {
	for _, route := range s.routeTable() {
		s.mux.HandleFunc(route.pattern, s.authorize(route.policy, route.handler))
	}
}

// routeTable lists every route. Reads are open; writes to settings and the
// daemon are the owner's; writes that act in a session follow its policy.
func (s *Server) routeTable() []route {
	return []route{
		{"GET /v1/health", open, s.handleHealth},
		{"GET /v1/server/status", open, s.handleServerStatus},
		{"POST /v1/admin/reload", owner, s.handleAdminReload},
		{"POST /v1/admin/restart", owner, s.handleAdminRestart},
		{"POST /v1/admin/stop", owner, s.handleAdminStop},

		{"GET /v1/setup/providers", open, s.handleSetupProviders},
		{"PATCH /v1/setup/providers/{id}", owner, s.handleSetupProviderUpdate},
		{"DELETE /v1/setup/providers/{id}", owner, s.handleSetupProviderDelete},
		{"POST /v1/setup/providers/{id}/models", owner, s.handleSetupProviderModels},
		{"GET /v1/session-providers", open, s.handleSessionProviders},
		{"GET /v1/external-agents", open, s.handleExternalAgents},
		{"PATCH /v1/external-agents/{id}", owner, s.handleExternalAgentUpdate},

		{"GET /v1/automation/jobs", open, s.handleAutomationJobs},
		{"POST /v1/automation/jobs", sessionInBody, s.handleAutomationJobCreate},
		{"GET /v1/automation/jobs/{id}", open, s.handleAutomationJob},
		{"DELETE /v1/automation/jobs/{id}", sessionOfJob, s.handleAutomationJobDelete},
		{"POST /v1/automation/jobs/{id}/{action}", sessionOfJob, s.handleAutomationJobAction},

		{"GET /v1/client-deliveries", open, s.handleClientDeliveries},
		{"POST /v1/client-deliveries/{id}/ack", open, s.handleClientDeliveryAck},
		{"POST /v1/client-deliveries/{id}/fail", open, s.handleClientDeliveryFail},

		{"GET /v1/sessions", open, s.handleSessions},
		{"POST /v1/sessions", open, s.handleSessionCreate},
		{"GET /v1/sessions/{id}", open, s.handleSession},
		{"PATCH /v1/sessions/{id}", sessionInPath, s.handleSessionRename},
		{"DELETE /v1/sessions/{id}", sessionInPath, s.handleSessionDelete},
		{"PATCH /v1/sessions/{id}/llm", sessionInPath, s.handleSessionLLMUpdate},
		{"GET /v1/sessions/{id}/models", open, s.handleSessionLLMModels},
		{"PATCH /v1/sessions/{id}/permissions", owner, s.handleSessionPermissionsUpdate},
		{"GET /v1/sessions/{id}/permission-rules", open, s.handleSessionPermissionRules},
		{"POST /v1/sessions/{id}/permission-rules", sessionInPath, s.handlePermissionRuleCreate},
		{"GET /v1/sessions/{id}/context", open, s.handleSessionContext},
		{"GET /v1/sessions/{id}/usage", open, s.handleSessionUsage},
		{"GET /v1/sessions/{id}/budget", open, s.handleSessionBudget},
		{"PUT /v1/sessions/{id}/budget", sessionInPath, s.handleSessionBudgetUpdate},
		{"GET /v1/sessions/{id}/todo", open, s.handleSessionTodo},
		{"DELETE /v1/sessions/{id}/todo", sessionInPath, s.handleSessionTodoClear},
		{"GET /v1/sessions/{id}/goal", open, s.handleSessionGoal},
		{"PUT /v1/sessions/{id}/goal", sessionInPath, s.handleSessionGoalSet},
		{"DELETE /v1/sessions/{id}/goal", sessionInPath, s.handleSessionGoalClear},
		{"POST /v1/sessions/{id}/goal/{action}", sessionInPath, s.handleSessionGoalAction},
		{"GET /v1/sessions/{id}/hooks", open, s.handleSessionHooks},
		{"POST /v1/sessions/{id}/hooks/trust", owner, s.handleSessionHooksTrust},
		{"GET /v1/sessions/{id}/tasks", open, s.handleSessionTasks},
		{"POST /v1/sessions/{id}/compact", sessionInPath, s.handleSessionCompact},
		{"POST /v1/sessions/{id}/clear", sessionInPath, s.handleSessionClear},
		{"POST /v1/sessions/{id}/system-message", sessionInPath, s.handleSessionSystemMessage},
		{"GET /v1/sessions/{id}/checkpoints", open, s.handleSessionCheckpoints},
		{"POST /v1/sessions/{id}/checkpoints/{checkpoint}/restore", owner, s.handleSessionCheckpointRestore},
		{"GET /v1/bindings/current", open, s.handleCurrentBinding},
		{"POST /v1/bindings/use", sessionInBody, s.handleUseBinding},
		{"GET /v1/snapshot", open, s.handleSnapshot},
		{"GET /v1/events", open, s.handleEvents},
		{"GET /v1/messages", open, s.handleMessages},
		{"POST /v1/messages", sessionInBody, s.handleMessageCreate},
		{"GET /v1/search", open, s.handleSearch},
		{"GET /v1/memory", open, s.handleMemory},
		{"POST /v1/memory/{id}/approve", owner, s.handleMemoryApprove},
		{"DELETE /v1/memory/{id}", owner, s.handleMemoryDelete},

		{"GET /v1/runs/{id}", open, s.handleRun},
		{"GET /v1/runs/{id}/steps", open, s.handleRunSteps},
		{"GET /v1/runs/{id}/progress", open, s.handleRunProgress},
		{"POST /v1/runs/{id}/cancel", sessionOfRun, s.handleRunCancel},
		{"GET /v1/tasks/{id}", open, s.handleTask},
		{"POST /v1/tasks/{id}/cancel", sessionOfTask, s.handleTaskCancel},
		{"GET /v1/approvals", open, s.handleApprovals},
		{"POST /v1/approvals/{id}/resolve", sessionOfApproval, s.handleApprovalResolve},
		{"DELETE /v1/permission-rules/{id}", sessionOfRule, s.handlePermissionRuleDelete},
		{"GET /v1/tools", open, s.handleTools},
		{"POST /v1/tools/execute", sessionInBody, s.handleToolExecute},

		{"GET /v1/modules", open, s.handleModules},
		{"GET /v1/settings/{module}", open, s.handleModuleSettings},
		{"POST /v1/settings/{module}", owner, s.handleModuleSettingsChange},
		{"GET /v1/modules/storage/files", open, s.handleStorageFiles},
		{"POST /v1/modules/storage/files", open, withBodyLimit(storageSaveJSONBodyLimitBytes, s.handleStorageFileCreate)},
		{"GET /v1/modules/storage/files/{path...}", open, s.handleStorageFile},
		{"DELETE /v1/modules/storage/files/{path...}", owner, s.handleStorageFileDelete},
		{"GET /v1/modules/storage/temp", open, s.handleStorageTemp},
		{"POST /v1/modules/storage/temp", open, withBodyLimit(storageSaveJSONBodyLimitBytes, s.handleStorageTempCreate)},
		{"POST /v1/modules/storage/temp/cleanup", owner, s.handleStorageTempCleanup},
		{"PATCH /v1/modules/storage/temp/settings", owner, s.handleStorageTempSettings},
		{"GET /v1/modules/storage/temp/{path...}", open, s.handleStorageTempFile},
		{"DELETE /v1/modules/storage/temp/{path...}", owner, s.handleStorageTempDelete},
		{"POST /v1/modules/storage/temp/{path}/promote", open, s.handleStorageTempPromote},
		{"POST /v1/modules/voice/tts", open, s.handleTextToSpeech},
		{"POST /v1/modules/voice/stt", open, withBodyLimit(voiceAudioJSONBodyLimitBytes, s.handleSpeechToText)},
		{"GET /v1/modules/voice/realtime_voice", open, s.handleRealtimeVoiceModule},
		{"POST /v1/realtime-voice/sessions", sessionInBody, s.handleRealtimeVoiceSessionCreate},
		{"GET /v1/realtime-voice/sessions/{id}", open, s.handleRealtimeVoiceSession},
		{"DELETE /v1/realtime-voice/sessions/{id}", open, s.handleRealtimeVoiceSessionClose},
		{"GET /v1/realtime-voice/sessions/{id}/stream", open, s.handleRealtimeVoiceStream},
		{"GET /v1/modules/mcp", open, s.handleMCP},
		{"PATCH /v1/modules/mcp", owner, s.handleMCPUpdate},
		{"POST /v1/modules/mcp/servers", owner, s.handleMCPServerCreate},
		{"PATCH /v1/modules/mcp/{id}/server", owner, s.handleMCPServerUpdate},
		{"DELETE /v1/modules/mcp/{id}/server", owner, s.handleMCPServerDelete},
		{"GET /v1/modules/skills", open, s.handleSkills},
		{"POST /v1/modules/skills", owner, s.handleSkillInstall},
		{"POST /v1/modules/skills/drafts", owner, s.handleSkillDraft},
		{"GET /v1/modules/skills/{id}", open, s.handleSkill},
		{"PATCH /v1/modules/skills/{id}", owner, s.handleSkillUpdate},
		{"PUT /v1/modules/skills/{id}/body", owner, s.handleSkillBody},
		{"POST /v1/modules/skills/{id}/{action}", owner, s.handleSkillAction},
		{"GET /v1/sessions/{id}/skills", open, s.handleSessionSkills},
		{"POST /v1/sessions/{id}/skills/{skill}", sessionInPath, s.handleSessionSkillUse},
		{"DELETE /v1/sessions/{id}/skills/{skill}", sessionInPath, s.handleSessionSkillUnload},
	}
}
