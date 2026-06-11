package tools

func CoreReadOnlyExecutors() []Executor {
	return executorsFromDefinitions(CoreDefinitionsFor(Policy{Profiles: []Profile{ProfileReadOnly}}))
}

func CoreCodingExecutors() []Executor {
	return executorsFromDefinitions(CoreDefinitionsFor(Policy{Profiles: []Profile{ProfileCoding}}))
}

func NewCoreReadOnlyRegistry(extra ...Executor) *Registry {
	executors := CoreReadOnlyExecutors()
	executors = append(executors, extra...)
	return NewRegistry(executors...)
}

func NewCoreCodingRegistry(extra ...Executor) *Registry {
	executors := CoreCodingExecutors()
	executors = append(executors, extra...)
	return NewRegistry(executors...)
}
