package tools

const (
	namespaceCoreFilesystem = "core.filesystem"
	namespaceCoreShell      = "core.shell"
	readToolName            = "read"
	globToolName            = "glob"
	grepToolName            = "grep"
	lsToolName              = "ls"
	defaultReadLimit        = 2000
	maxReadBytes            = 5 * 1024 * 1024
	defaultSearchLimit      = 100
	defaultListDepth        = 3
	maxListEntries          = 1000
	maxRenderedLineWidth    = 500
)

type ReadParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ReadResponseMetadata struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type GlobResponseMetadata struct {
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
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type LSParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
	Depth  int      `json:"depth,omitempty"`
}

type LSResponseMetadata struct {
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

type readExecutor struct{}
type globExecutor struct{}
type grepExecutor struct{}
type lsExecutor struct{}

func NewReadExecutor() Executor { return &readExecutor{} }
func NewGlobExecutor() Executor { return &globExecutor{} }
func NewGrepExecutor() Executor { return &grepExecutor{} }
func NewLSExecutor() Executor   { return &lsExecutor{} }

func (e *readExecutor) Spec() Spec {
	return Spec{
		ID:              readToolName,
		Name:            "Read",
		Description:     "Read a file with line numbers",
		Risk:            RiskSafe,
		Namespace:       namespaceCoreFilesystem,
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
		OutputKind:      OutputFileContent,
		InputJSONSchema: readInputSchema,
	}
}

func (e *globExecutor) Spec() Spec {
	return Spec{
		ID:              globToolName,
		Name:            "Glob",
		Description:     "Find files by path pattern",
		Risk:            RiskSafe,
		Namespace:       namespaceCoreFilesystem,
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
		OutputKind:      OutputSearchResults,
		InputJSONSchema: globInputSchema,
	}
}

func (e *grepExecutor) Spec() Spec {
	return Spec{
		ID:              grepToolName,
		Name:            "Grep",
		Description:     "Search file contents by pattern",
		Risk:            RiskSafe,
		Namespace:       namespaceCoreFilesystem,
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
		OutputKind:      OutputSearchResults,
		InputJSONSchema: grepInputSchema,
	}
}

func (e *lsExecutor) Spec() Spec {
	return Spec{
		ID:              lsToolName,
		Name:            "LS",
		Description:     "List files in a tree",
		Risk:            RiskSafe,
		Namespace:       namespaceCoreFilesystem,
		Category:        CategoryFilesystem,
		Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
		OutputKind:      OutputFileTree,
		InputJSONSchema: lsInputSchema,
	}
}
