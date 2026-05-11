package tools

func newReadOnlyRegistry() *Registry {
	return NewRegistry(
		NewReadExecutor(),
		NewGlobExecutor(),
		NewGrepExecutor(),
		NewLSExecutor(),
	)
}

func newCoreCodingRegistry() *Registry {
	return NewRegistry(
		NewReadExecutor(),
		NewGlobExecutor(),
		NewGrepExecutor(),
		NewLSExecutor(),
		NewWriteExecutor(),
		NewEditExecutor(),
		NewMultiEditExecutor(),
		NewBashExecutor(),
		NewJobOutputExecutor(),
		NewJobKillExecutor(),
	)
}
