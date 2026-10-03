package tools

// Definition is a core tool; one whose executor needs the daemon's services
// (the shell tools) has no NewExecutor and is registered on its own.
type Definition struct {
	Spec        Spec
	NewExecutor func() Executor
}

// CoreExecutors returns the core tools that need no daemon services.
func CoreExecutors() []Executor {
	executors := make([]Executor, 0, len(coreDefinitions))
	for _, definition := range coreDefinitions {
		if definition.NewExecutor != nil {
			executors = append(executors, definition.NewExecutor())
		}
	}
	return executors
}

func coreDefinitionSpec(id string) Spec {
	for _, definition := range coreDefinitions {
		if definition.Spec.ID == id {
			return cloneSpec(definition.Spec)
		}
	}
	return Spec{}
}

var coreDefinitions = []Definition{
	{
		Spec: Spec{
			ID:              readToolName,
			Description:     "Read a file with line numbers",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: readInputSchema,
		},
		NewExecutor: NewReadExecutor,
	},
	{
		Spec: Spec{
			ID:              globToolName,
			Description:     "Find files by path pattern",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: globInputSchema,
		},
		NewExecutor: NewGlobExecutor,
	},
	{
		Spec: Spec{
			ID:              grepToolName,
			Description:     "Search file contents by pattern",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: grepInputSchema,
		},
		NewExecutor: NewGrepExecutor,
	},
	{
		Spec: Spec{
			ID:              lsToolName,
			Description:     "List files in a tree",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: lsInputSchema,
		},
		NewExecutor: NewLSExecutor,
	},
	{
		Spec: Spec{
			ID:              writeToolName,
			Description:     "Create or replace a file",
			Effect:          EffectMutation,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: writeInputSchema,
		},
		NewExecutor: NewWriteExecutor,
	},
	{
		Spec: Spec{
			ID:              editToolName,
			Description:     "Replace content inside an existing file",
			Effect:          EffectMutation,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: editInputSchema,
		},
		NewExecutor: NewEditExecutor,
	},
	{
		Spec: Spec{
			ID:              multiEditToolName,
			Description:     "Apply several edits to one file",
			Effect:          EffectMutation,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: multiEditInputSchema,
		},
		NewExecutor: NewMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:              bashToolName,
			Description:     "Run a shell command",
			Effect:          EffectMutation,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: bashInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:              taskOutputToolName,
			Description:     "Read a background task's new output",
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: taskOutputInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:              taskKillToolName,
			Description:     "Stop a background task",
			Effect:          EffectMutation,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: taskKillInputSchema,
		},
	},
}
