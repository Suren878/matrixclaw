package clientruntime

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type DaemonClientFunc func(externalKey string) (*daemonclient.Client, error)

type ControlplaneRuntime struct {
	Client      string
	ExternalKey string
	WorkingDir  string
	Daemon      DaemonClientFunc
}

func (r ControlplaneRuntime) ClientName() string {
	return strings.TrimSpace(r.Client)
}

func (r ControlplaneRuntime) client(externalKey string) (*daemonclient.Client, error) {
	if r.Daemon == nil {
		return nil, fmt.Errorf("daemon client is not configured")
	}
	externalKey = strings.TrimSpace(externalKey)
	if externalKey == "" {
		externalKey = strings.TrimSpace(r.ExternalKey)
	}
	return r.Daemon(externalKey)
}

func (r ControlplaneRuntime) CurrentBinding(ctx context.Context, externalKey string) (core.ClientBinding, error) {
	client, err := r.client(externalKey)
	if err != nil {
		return core.ClientBinding{}, err
	}
	binding, err := client.CurrentBinding(ctx)
	if err != nil && daemonclient.IsAPIStatus(err, http.StatusNotFound) {
		return core.ClientBinding{}, nil
	}
	return binding, err
}

func (r ControlplaneRuntime) ListSessions(ctx context.Context) ([]core.Session, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.ListSessions(ctx)
}

func (r ControlplaneRuntime) CreateSession(ctx context.Context, externalKey string, title string, workingDir string) (core.Session, error) {
	return r.CreateSessionWithOptions(ctx, externalKey, core.CreateSessionRequest{
		Title:      title,
		WorkingDir: workingDir,
	})
}

func (r ControlplaneRuntime) CreateSessionWithOptions(ctx context.Context, externalKey string, request core.CreateSessionRequest) (core.Session, error) {
	client, err := r.client(externalKey)
	if err != nil {
		return core.Session{}, err
	}
	if strings.TrimSpace(request.WorkingDir) == "" {
		request.WorkingDir = r.WorkingDir
	}
	session, err := client.CreateSessionWithRequest(ctx, request)
	if err != nil {
		return core.Session{}, err
	}
	if _, err := client.UseSession(ctx, session.ID); err != nil {
		return core.Session{}, err
	}
	return session, nil
}

func (r ControlplaneRuntime) ListExternalAgents(ctx context.Context) ([]core.ExternalAgentDescriptor, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.ListExternalAgents(ctx)
}

func (r ControlplaneRuntime) UpdateExternalAgent(ctx context.Context, agentID string, update core.UpdateExternalAgentRequest) ([]core.ExternalAgentDescriptor, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.UpdateExternalAgent(ctx, agentID, update)
}

func (r ControlplaneRuntime) VoiceModules(ctx context.Context) ([]setup.VoiceModuleDescriptor, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.VoiceModules(ctx)
}

func (r ControlplaneRuntime) UpdateVoiceModule(ctx context.Context, moduleID string, update setup.VoiceModuleUpdate) ([]setup.VoiceModuleDescriptor, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.UpdateVoiceModule(ctx, moduleID, update)
}

func (r ControlplaneRuntime) VoiceProviderAction(ctx context.Context, moduleID string, providerID string, request setup.VoiceProviderActionRequest) (setup.VoiceProviderOption, error) {
	client, err := r.client("")
	if err != nil {
		return setup.VoiceProviderOption{}, err
	}
	return client.VoiceProviderAction(ctx, moduleID, providerID, request)
}

func (r ControlplaneRuntime) UseSession(ctx context.Context, externalKey string, sessionID string) (core.ClientBinding, error) {
	client, err := r.client(externalKey)
	if err != nil {
		return core.ClientBinding{}, err
	}
	return client.UseSession(ctx, sessionID)
}

func (r ControlplaneRuntime) RenameSession(ctx context.Context, sessionID string, title string) (core.Session, error) {
	client, err := r.client("")
	if err != nil {
		return core.Session{}, err
	}
	return client.RenameSession(ctx, sessionID, title)
}

func (r ControlplaneRuntime) DeleteSession(ctx context.Context, sessionID string) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.DeleteSession(ctx, sessionID)
}

func (r ControlplaneRuntime) ListSetupProviders(ctx context.Context) ([]setup.ProviderSetupItem, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.ListSetupProviders(ctx)
}

func (r ControlplaneRuntime) ConfigureSetupProvider(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) (setup.ProviderSetupItem, error) {
	client, err := r.client("")
	if err != nil {
		return setup.ProviderSetupItem{}, err
	}
	return client.ConfigureSetupProvider(ctx, providerID, update)
}

func (r ControlplaneRuntime) ProviderModels(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) ([]string, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.ProviderModels(ctx, providerID, update)
}

func (r ControlplaneRuntime) DeleteSetupProvider(ctx context.Context, providerID string) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.DeleteSetupProvider(ctx, providerID)
}

func (r ControlplaneRuntime) ServerStatus(ctx context.Context) (core.ServerStatus, error) {
	client, err := r.client("")
	if err != nil {
		return core.ServerStatus{}, err
	}
	return client.ServerStatus(ctx)
}

func (r ControlplaneRuntime) RestartDaemon(ctx context.Context) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.RestartDaemon(ctx)
}

func (r ControlplaneRuntime) StopDaemon(ctx context.Context) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.StopDaemon(ctx)
}

func (r ControlplaneRuntime) RestartDaemonWithNotification(ctx context.Context) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	clientName := r.ClientName()
	if clientName == "" {
		clientName = strings.TrimSpace(client.ClientName)
	}
	externalKey := strings.TrimSpace(r.ExternalKey)
	if externalKey == "" {
		externalKey = strings.TrimSpace(client.ExternalKey)
	}
	return client.RestartDaemonWithNotification(ctx, core.ClientDeliveryTarget{
		Client:      clientName,
		ExternalKey: externalKey,
	})
}

func (r ControlplaneRuntime) ListClientDeliveries(ctx context.Context, filter core.ClientDeliveryFilter) ([]core.ClientDelivery, error) {
	client, err := r.client(filter.ExternalKey)
	if err != nil {
		return nil, err
	}
	return client.ListClientDeliveries(ctx, filter)
}

func (r ControlplaneRuntime) AcknowledgeClientDelivery(ctx context.Context, deliveryID string) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.AcknowledgeClientDelivery(ctx, deliveryID)
}

func (r ControlplaneRuntime) CreateAutomationJob(ctx context.Context, input automation.CreateJobInput) (automation.Job, error) {
	if strings.TrimSpace(input.Client) == "" {
		input.Client = r.ClientName()
	}
	if strings.TrimSpace(input.ExternalKey) == "" {
		input.ExternalKey = strings.TrimSpace(r.ExternalKey)
	}
	client, err := r.client(input.ExternalKey)
	if err != nil {
		return automation.Job{}, err
	}
	return client.CreateAutomationJob(ctx, input)
}

func (r ControlplaneRuntime) ListAutomationJobs(ctx context.Context) ([]automation.Job, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.ListAutomationJobs(ctx)
}

func (r ControlplaneRuntime) PauseAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	client, err := r.client("")
	if err != nil {
		return automation.Job{}, err
	}
	return client.PauseAutomationJob(ctx, jobID)
}

func (r ControlplaneRuntime) ResumeAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	client, err := r.client("")
	if err != nil {
		return automation.Job{}, err
	}
	return client.ResumeAutomationJob(ctx, jobID)
}

func (r ControlplaneRuntime) CompleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	client, err := r.client("")
	if err != nil {
		return automation.Job{}, err
	}
	return client.CompleteAutomationJob(ctx, jobID)
}

func (r ControlplaneRuntime) DeleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	client, err := r.client("")
	if err != nil {
		return automation.Job{}, err
	}
	return client.DeleteAutomationJob(ctx, jobID)
}

func (r ControlplaneRuntime) RunAutomationJobNow(ctx context.Context, jobID string) (automation.Fire, error) {
	client, err := r.client("")
	if err != nil {
		return automation.Fire{}, err
	}
	return client.RunAutomationJobNow(ctx, jobID)
}

func (r ControlplaneRuntime) UpdateSessionProvider(ctx context.Context, sessionID string, providerID string) (core.Session, error) {
	client, err := r.client("")
	if err != nil {
		return core.Session{}, err
	}
	return client.UpdateSessionProvider(ctx, sessionID, providerID)
}

func (r ControlplaneRuntime) UpdateSessionPermissionMode(ctx context.Context, sessionID string, mode core.PermissionMode) (core.Session, error) {
	client, err := r.client("")
	if err != nil {
		return core.Session{}, err
	}
	return client.UpdateSessionPermissionMode(ctx, sessionID, mode)
}

func (r ControlplaneRuntime) SessionContext(ctx context.Context, sessionID string) (core.ContextReport, error) {
	client, err := r.client("")
	if err != nil {
		return core.ContextReport{}, err
	}
	return client.SessionContext(ctx, sessionID)
}

func (r ControlplaneRuntime) SessionUsage(ctx context.Context, sessionID string) (core.UsageReport, error) {
	client, err := r.client("")
	if err != nil {
		return core.UsageReport{}, err
	}
	return client.SessionUsage(ctx, sessionID)
}

func (r ControlplaneRuntime) SessionPlan(ctx context.Context, sessionID string) (core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionPlan{}, err
	}
	return client.SessionPlan(ctx, sessionID)
}

func (r ControlplaneRuntime) SetSessionGoal(ctx context.Context, sessionID string, goal string) (core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionPlan{}, err
	}
	return client.SetSessionGoal(ctx, sessionID, goal)
}

func (r ControlplaneRuntime) ClearSessionPlan(ctx context.Context, sessionID string) (core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionPlan{}, err
	}
	return client.ClearSessionPlan(ctx, sessionID)
}

func (r ControlplaneRuntime) AddPlanItem(ctx context.Context, sessionID string, text string, parentID string) (core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionPlan{}, err
	}
	return client.AddPlanItemWithParent(ctx, sessionID, text, parentID)
}

func (r ControlplaneRuntime) UpdatePlanItem(ctx context.Context, sessionID string, itemID string, status core.PlanItemStatus, text string) (core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.SessionPlan{}, err
	}
	return client.UpdatePlanItem(ctx, sessionID, itemID, status, text)
}

func (r ControlplaneRuntime) StartSessionPlanRun(ctx context.Context, sessionID string, reset bool) (core.PlanRun, core.SessionPlan, error) {
	client, err := r.client("")
	if err != nil {
		return core.PlanRun{}, core.SessionPlan{}, err
	}
	return client.StartSessionPlanRun(ctx, sessionID, reset)
}

func (r ControlplaneRuntime) BindSessionPlanRunStep(ctx context.Context, sessionID string, runID string) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	_, _, err = client.BindSessionPlanRunStep(ctx, sessionID, runID)
	return err
}

func (r ControlplaneRuntime) Search(ctx context.Context, filter core.SearchFilter) (core.SearchReport, error) {
	client, err := r.client("")
	if err != nil {
		return core.SearchReport{}, err
	}
	return client.Search(ctx, filter)
}

func (r ControlplaneRuntime) CompactSession(ctx context.Context, sessionID string) (core.CompactSessionResult, error) {
	client, err := r.client("")
	if err != nil {
		return core.CompactSessionResult{}, err
	}
	return client.CompactSession(ctx, sessionID)
}

func (r ControlplaneRuntime) CreateSystemMessage(ctx context.Context, sessionID string, content string) (core.Message, error) {
	client, err := r.client("")
	if err != nil {
		return core.Message{}, err
	}
	return client.CreateSystemMessage(ctx, sessionID, content)
}

func (r ControlplaneRuntime) SaveStorageFile(ctx context.Context, storagePath string, content []byte, title string, tags []string, mimeType string) (localstorage.Entry, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.Entry{}, err
	}
	return client.SaveStorageFile(ctx, storagePath, content, title, tags, mimeType)
}

func (r ControlplaneRuntime) ListTemporaryStorageFiles(ctx context.Context, limit int) (localstorage.TempListResult, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.TempListResult{}, err
	}
	return client.ListTemporaryStorageFiles(ctx, limit)
}

func (r ControlplaneRuntime) PromoteTemporaryStorageFile(ctx context.Context, tempPath string, destPath string) (localstorage.Entry, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.Entry{}, err
	}
	return client.PromoteTemporaryStorageFile(ctx, tempPath, destPath)
}

func (r ControlplaneRuntime) DeleteTemporaryStorageFile(ctx context.Context, tempPath string) (localstorage.TempEntry, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.TempEntry{}, err
	}
	return client.DeleteTemporaryStorageFile(ctx, tempPath)
}

func (r ControlplaneRuntime) CleanupTemporaryStorageFiles(ctx context.Context) (localstorage.CleanupResult, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.CleanupResult{}, err
	}
	return client.CleanupTemporaryStorageFiles(ctx)
}

func (r ControlplaneRuntime) UpdateTemporaryStorageSettings(ctx context.Context, autoCleanup *bool, ttlDays int64, maxGB float64) (localstorage.TempSettings, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.TempSettings{}, err
	}
	return client.UpdateTemporaryStorageSettings(ctx, autoCleanup, ttlDays, maxGB)
}

func (r ControlplaneRuntime) ListStorageFiles(ctx context.Context, filter localstorage.ListFilter) (localstorage.ListResult, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.ListResult{}, err
	}
	return client.ListStorageFiles(ctx, filter)
}

func (r ControlplaneRuntime) ReadStorageFile(ctx context.Context, storagePath string) (localstorage.ReadResult, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.ReadResult{}, err
	}
	return client.ReadStorageFile(ctx, storagePath)
}

func (r ControlplaneRuntime) DeleteStorageFile(ctx context.Context, storagePath string) (localstorage.Entry, error) {
	client, err := r.client("")
	if err != nil {
		return localstorage.Entry{}, err
	}
	return client.DeleteStorageFile(ctx, storagePath)
}
