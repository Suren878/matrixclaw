package tools

// MaxReadBytes is the largest file the read tool opens.
const MaxReadBytes = 5 * 1024 * 1024

const (
	namespaceCoreFilesystem = "core.filesystem"
	namespaceCoreShell      = "core.shell"
	namespaceCoreWeb        = "core.web"
	readToolName            = "read"
	defaultReadLimit        = 2000
)

type ReadParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ReadResponseMetadata struct {
	FilesystemPathMetadata
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type readExecutor struct{}

func NewReadExecutor() Executor { return &readExecutor{} }

func (e *readExecutor) Spec() Spec {
	return coreDefinitionSpec(readToolName)
}
