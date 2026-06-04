package tools

type CoreRegistryOptions struct {
	Web *WebService
}

func CoreReadOnlyExecutors() []Executor {
	return executorsFromDefinitions(CoreDefinitionsFor(Policy{Profiles: []Profile{ProfileReadOnly}}))
}

func CoreCodingExecutors() []Executor {
	return CoreCodingExecutorsWithOptions(CoreRegistryOptions{})
}

func CoreCodingExecutorsWithOptions(options CoreRegistryOptions) []Executor {
	definitions := CoreDefinitionsFor(Policy{Profiles: []Profile{ProfileCoding}})
	executors := make([]Executor, 0, len(definitions))
	for _, definition := range definitions {
		if normalizeToolID(definition.Spec.ID) == webFetchToolName && options.Web != nil {
			executors = append(executors, NewWebFetchExecutorWithService(options.Web))
			continue
		}
		if executor := definition.Executor(); executor != nil {
			executors = append(executors, executor)
		}
	}
	return executors
}

func NewCoreReadOnlyRegistry(extra ...Executor) *Registry {
	executors := CoreReadOnlyExecutors()
	executors = append(executors, extra...)
	return NewRegistry(executors...)
}

func NewCoreCodingRegistry(extra ...Executor) *Registry {
	return NewCoreCodingRegistryWithOptions(CoreRegistryOptions{}, extra...)
}

func NewCoreCodingRegistryWithOptions(options CoreRegistryOptions, extra ...Executor) *Registry {
	executors := CoreCodingExecutorsWithOptions(options)
	executors = append(executors, extra...)
	return NewRegistry(executors...)
}
