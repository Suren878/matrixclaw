package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// nativeTurn is what the per-step prompt and tool list of a native run depend
// on; Continues lists the runs this one continues, latest first.
type nativeTurn struct {
	RunID      string
	Continues  []string
	SessionID  string
	WorkingDir string
	Subagent   bool
	Readonly   bool
	// Agent is a subagent's profile and OutputSchema the schema its final
	// answer must match, if any.
	Agent        AgentProfile
	OutputSchema json.RawMessage
	// Goal is set when the session had an active goal as the run started.
	Goal               bool
	ClientCapabilities ClientCapabilities
	ToolUse            bool
	// Model names the provider and model the run talks to.
	Model string
	// Plan is set while the session is in plan mode; PatchEdits when the
	// model gets apply_patch instead of edit and multiedit.
	Plan       bool
	PatchEdits bool
}

// coreTools is the Tools port of one native run; interrupted is the tool
// batch the run's interrupted execution last checkpointed.
type coreTools struct {
	c           *Core
	turn        nativeTurn
	batch       *toolBatch
	interrupted *agent.ToolBatch
}

// toolBatch is what the Journal and Tools ports of one run share about the
// calls being admitted, all on the engine goroutine. Every engine checkpoint
// ends a group of admitted calls, whose scope is read once; the checkpoint
// before a model call starts a new batch, whose first call that may change
// files takes the file checkpoint.
type toolBatch struct {
	scope    *callScope
	snapshot bool
}

// checkpointed is told of each engine checkpoint.
func (b *toolBatch) checkpointed(phase agent.Phase) {
	b.scope = nil
	if phase == agent.PhaseModel {
		b.snapshot = false
	}
}

// coreGrant is a call Authorize allowed, as Execute runs it.
type coreGrant struct {
	prepared preparedToolCall
	input    ExecuteToolInput
	check    callPermission
}

func (t coreTools) Specs(ctx context.Context) []tools.Spec {
	specs := t.c.nativeToolSpecs(t.turn)
	for i := range specs {
		if specs[i].ID == agentToolName {
			specs[i] = t.c.agentToolSpec(ctx, specs[i], t.turn)
		}
	}
	return specs
}

// scope is the run session's call scope, read once per group of calls.
func (t coreTools) scope(ctx context.Context) (callScope, error) {
	if t.batch.scope == nil {
		scope, err := t.c.callScope(ctx, t.turn.SessionID)
		if err != nil {
			return callScope{}, err
		}
		t.batch.scope = &scope
	}
	return *t.batch.scope, nil
}

// Authorize rejects invalid calls and those a deny rule blocks; the grant
// keeps the verdict Execute acts on. A mutating call that waits for approval
// is a barrier, and so is a child writing the parent's directory. A blocking
// child's time is its own. The batch's first call that may change files takes
// its file checkpoint before any call of the batch starts.
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Admission, error) {
	scope, err := t.scope(ctx)
	if err != nil {
		return agent.Admission{}, err
	}
	spec, err := t.c.checkToolCall(scope, name, call)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Admission{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Admission{}, err
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = scope.session.WorkingDir
	}
	check, err := t.c.permitCall(ctx, scope, name, spec, call)
	if err != nil {
		return agent.Admission{}, err
	}
	if refusal, refused := check.refusal(); refused {
		return agent.Admission{Reason: refusal}, nil
	}
	grant := coreGrant{
		prepared: preparedToolCall{SessionID: call.SessionID, RunID: call.RunID, ToolName: name, Spec: spec, ToolCallID: call.ToolCallID, WorkingDir: call.WorkingDir},
		input:    ExecuteToolInput{Client: call.Client, ExternalKey: call.ExternalKey, Args: call.Args},
		check:    check,
	}
	call = check.input(call)
	t.checkpointBatch(ctx, spec, call)
	admission := agent.Admission{Allowed: true, Barrier: spec.Mutates() && check.ask, Key: t.c.tools.ConcurrencyKey(spec.ID, call), Grant: grant}
	if spec.ID == agentToolName {
		admission.Barrier = t.c.agentCallWritesSharedDir(call)
		admission.Delegated = !parseAgentInput(call.Args).Background
	}
	return admission, nil
}

// Execute runs a call as Authorize granted it; a tool's failure is its result.
func (t coreTools) Execute(ctx context.Context, grant agent.Grant) (tools.Result, *tools.ApprovalRequest, error) {
	g := grant.(coreGrant)
	result, ask, err := t.c.runToolCall(ctx, g.prepared, g.input, g.check)
	if err != nil {
		result = t.c.toolFailure(g.prepared.SessionID, err)
	}
	return result, ask, nil
}

// lspToolName is offered only where a language server serves the project.
const lspToolName = "lsp"

// nativeToolSpecs lists the tools a native run may see; nil without a registry.
// One edit tool family is offered, the one the model was trained on,
// exit_plan_mode only in plan mode and lsp only where a server serves the
// project.
func (c *Core) nativeToolSpecs(turn nativeTurn) []tools.Spec {
	if c.tools == nil {
		return nil
	}
	specs := c.tools.List()
	out := make([]tools.Spec, 0, len(specs))
	for _, spec := range specs {
		if turn.Subagent && (!subagentToolAllowed(spec) || !turn.Agent.allows(spec.ID)) || goalTools[spec.ID] && !turn.Goal {
			continue
		}
		switch spec.ID {
		case "edit", "multiedit":
			if turn.PatchEdits {
				continue
			}
		case "apply_patch":
			if !turn.PatchEdits {
				continue
			}
		case exitPlanModeToolName:
			if !turn.Plan {
				continue
			}
		case lspToolName:
			if c.lspAvailable == nil || !c.lspAvailable(turn.WorkingDir) {
				continue
			}
		}
		if turn.Readonly && spec.Mutates() {
			readOnly, ok := tools.ReadOnlySpec(spec)
			if !ok {
				continue
			}
			spec = readOnly
		}
		if spec.ID == "text_to_speech" && !clientSupportsVoiceDelivery(turn.ClientCapabilities) {
			continue
		}
		if spec.ID == "send_file" && !clientSupportsDocumentDelivery(turn.ClientCapabilities) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func clientSupportsVoiceDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsVoiceDelivery
}

func clientSupportsDocumentDelivery(capabilities ClientCapabilities) bool {
	return capabilities.SupportsDocumentDelivery
}
