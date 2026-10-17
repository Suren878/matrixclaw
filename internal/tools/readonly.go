package tools

// MaxReadBytes is the largest file grep searches.
const MaxReadBytes = 5 * 1024 * 1024

const (
	namespaceCoreFilesystem = "core.filesystem"
	namespaceCoreShell      = "core.shell"
	namespaceCoreWeb        = "core.web"
	globToolName            = "glob"
	grepToolName            = "grep"
	lsToolName              = "ls"
	defaultSearchLimit      = 100
	defaultListDepth        = 3
	maxListEntries          = 1000
	maxRenderedLineWidth    = 500
)

type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type GlobResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	Include     string `json:"include,omitempty"`
	LiteralText bool   `json:"literal_text,omitempty"`
}

type GrepResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type LSParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
	Depth  int      `json:"depth,omitempty"`
}

type LSResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type NodeType string

const (
	NodeTypeFile      NodeType = "file"
	NodeTypeDirectory NodeType = "directory"
)

type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     NodeType    `json:"type"`
	Children []*TreeNode `json:"children,omitempty"`
}

type globExecutor struct{}
type grepExecutor struct{}
type lsExecutor struct{}

func NewGlobExecutor() Executor { return &globExecutor{} }
func NewGrepExecutor() Executor { return &grepExecutor{} }
func NewLSExecutor() Executor   { return &lsExecutor{} }

func (e *globExecutor) Spec() Spec {
	return coreDefinitionSpec(globToolName)
}

func (e *grepExecutor) Spec() Spec {
	return coreDefinitionSpec(grepToolName)
}

func (e *lsExecutor) Spec() Spec {
	return coreDefinitionSpec(lsToolName)
}
