package tools

// Definition is a core tool; one whose executor needs the daemon's services
// (the shell tools) has no constructor and is registered on its own. The file
// tools that read and change files share one fileLedger.
type Definition struct {
	Spec            Spec
	NewExecutor     func() Executor
	newFileExecutor func(files *fileLedger) Executor
}

// CoreExecutors returns the core tools that need no daemon services.
func CoreExecutors() []Executor {
	files := newFileLedger()
	executors := make([]Executor, 0, len(coreDefinitions))
	for _, definition := range coreDefinitions {
		switch {
		case definition.newFileExecutor != nil:
			executors = append(executors, definition.newFileExecutor(files))
		case definition.NewExecutor != nil:
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
			Description:     readDescription,
			Effect:          EffectReadOnly,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: readInputSchema,
		},
		newFileExecutor: newReadExecutor,
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
			Description:     writeDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: writeInputSchema,
		},
		newFileExecutor: newWriteExecutor,
	},
	{
		Spec: Spec{
			ID:              editToolName,
			Description:     editDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: editInputSchema,
		},
		newFileExecutor: newEditExecutor,
	},
	{
		Spec: Spec{
			ID:              multiEditToolName,
			Description:     multiEditDescription,
			Effect:          EffectMutation,
			Asks:            true,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			InputJSONSchema: multiEditInputSchema,
		},
		newFileExecutor: newMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:              bashToolName,
			Description:     "Run a shell command",
			Effect:          EffectMutation,
			Asks:            true,
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
			Asks:            true,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			InputJSONSchema: taskKillInputSchema,
		},
	},
}

const (
	readDescription = `Read a text file. Each line comes back prefixed with its line number and a tab; the prefix is not part of the file, so never copy it into edit strings. ` +
		`One call returns at most 2000 lines and about 24 KB; when more follows, the result ends with the offset to pass next. Lines over 2000 characters are cut. ` +
		`For a long file, locate the part you need with grep first and read it with offset and limit. Binary files are refused. ` +
		`Read a file before you change it with write, edit or multiedit; read several files in one reply when you need them. For directories use ls.`
	writeDescription = `Create a file, or replace an existing one completely, with content. Missing parent directories are created. ` +
		`An existing file must have been read in this session and not changed since, or the call is refused. ` +
		`To change part of a file use edit or multiedit instead: they send only the changed text and cannot drop the rest by accident. ` +
		`Do not create documentation or README files unless asked.`
	editDescription = `Replace a piece of text in an existing file. old_string should be copied from read output exactly (indentation, whitespace and line breaks, without the line-number prefix) ` +
		`and must occur exactly once unless replace_all is true; include enough surrounding lines to make it unique. new_string must differ from old_string. ` +
		`If old_string is not found byte for byte, a match ignoring indentation or surrounding whitespace is tried, and the result says so; a miss shows the closest lines. ` +
		`The file must have been read in this session and not changed since. The result shows the changed lines with their line numbers, so there is no need to read the file again to check. Use write to create files.`
	multiEditDescription = `Apply several replacements to one file in one atomic call: edits apply in order, each to the result of the one before, and if any edit fails nothing is written. ` +
		`Each edit follows the rules of edit. Prefer this to several edit calls on the same file.`
)
