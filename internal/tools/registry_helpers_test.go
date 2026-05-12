package tools

func newReadOnlyRegistry() *Registry {
	return NewCoreReadOnlyRegistry()
}

func newCoreCodingRegistry() *Registry {
	return NewCoreCodingRegistry()
}
