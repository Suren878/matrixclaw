package tools

type Definition struct {
	Spec        Spec
	NewExecutor func() Executor
}

func (d Definition) Executor() Executor {
	if d.NewExecutor == nil {
		return nil
	}
	return d.NewExecutor()
}

func CoreDefinitions() []Definition {
	return cloneDefinitions(coreDefinitions)
}

func CoreDefinitionsFor(policy Policy) []Definition {
	policy = normalizePolicy(policy)
	definitions := CoreDefinitions()
	out := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if !policy.Allows(definition.Spec) {
			continue
		}
		out = append(out, definition)
	}
	return out
}

func CoreSpec(toolID string) (Spec, bool) {
	spec := coreDefinitionSpec(toolID)
	return spec, spec.ID != ""
}

func executorsFromDefinitions(definitions []Definition) []Executor {
	executors := make([]Executor, 0, len(definitions))
	for _, definition := range definitions {
		if executor := definition.Executor(); executor != nil {
			executors = append(executors, executor)
		}
	}
	return executors
}

func cloneDefinitions(definitions []Definition) []Definition {
	out := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		definition.Spec = cloneSpec(definition.Spec)
		out = append(out, definition)
	}
	return out
}

func coreDefinitionSpec(id string) Spec {
	id = normalizeToolID(id)
	if id == "multi_edit" {
		id = multiEditToolName
	}
	for _, definition := range coreDefinitions {
		if normalizeToolID(definition.Spec.ID) == id {
			return cloneSpec(definition.Spec)
		}
	}
	return Spec{}
}

var coreDefinitions = []Definition{
	{
		Spec: Spec{
			ID:              readToolName,
			Name:            "Read",
			Description:     "Read a file with line numbers",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputFileContent,
			InputJSONSchema: readInputSchema,
		},
		NewExecutor: NewReadExecutor,
	},
	{
		Spec: Spec{
			ID:              globToolName,
			Name:            "Glob",
			Description:     "Find files by path pattern",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: globInputSchema,
		},
		NewExecutor: NewGlobExecutor,
	},
	{
		Spec: Spec{
			ID:              grepToolName,
			Name:            "Grep",
			Description:     "Search file contents by pattern",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: grepInputSchema,
		},
		NewExecutor: NewGrepExecutor,
	},
	{
		Spec: Spec{
			ID:              lsToolName,
			Name:            "LS",
			Description:     "List files in a tree",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputFileTree,
			InputJSONSchema: lsInputSchema,
		},
		NewExecutor: NewLSExecutor,
	},
	{
		Spec: Spec{
			ID:               writeToolName,
			Name:             "Write",
			Description:      "Create or replace a file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "write_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  writeInputSchema,
		},
		NewExecutor: NewWriteExecutor,
	},
	{
		Spec: Spec{
			ID:               editToolName,
			Name:             "Edit",
			Description:      "Replace content inside an existing file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "edit_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  editInputSchema,
		},
		NewExecutor: NewEditExecutor,
	},
	{
		Spec: Spec{
			ID:               multiEditToolName,
			Name:             "MultiEdit",
			Description:      "Apply several edits to one file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "multi_edit_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  multiEditInputSchema,
		},
		NewExecutor: NewMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:               bashToolName,
			Name:             "Bash",
			Description:      "Run a shell command",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "bash_permissions",
			Namespace:        namespaceCoreShell,
			Category:         CategoryShell,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputText,
			InputJSONSchema:  bashInputSchema,
		},
		NewExecutor: NewBashExecutor,
	},
	{
		Spec: Spec{
			ID:              jobOutputToolName,
			Name:            "JobOutput",
			Description:     "Read background job output",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			Profiles:        []Profile{ProfileCoding},
			OutputKind:      OutputJob,
			InputJSONSchema: jobOutputInputSchema,
		},
		NewExecutor: NewJobOutputExecutor,
	},
	{
		Spec: Spec{
			ID:               jobKillToolName,
			Name:             "JobKill",
			Description:      "Kill a background job",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "job_kill",
			Namespace:        namespaceCoreShell,
			Category:         CategoryShell,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputJob,
			InputJSONSchema:  jobKillInputSchema,
		},
		NewExecutor: NewJobKillExecutor,
	},
}
