package tools

func CoreReadOnlyExecutors() []Executor {
	return []Executor{
		NewReadExecutor(),
		NewGlobExecutor(),
		NewGrepExecutor(),
		NewLSExecutor(),
	}
}

func CoreCodingExecutors() []Executor {
	executors := CoreReadOnlyExecutors()
	executors = append(executors,
		NewWriteExecutor(),
		NewEditExecutor(),
		NewMultiEditExecutor(),
		NewBashExecutor(),
		NewJobOutputExecutor(),
		NewJobKillExecutor(),
	)
	return executors
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
