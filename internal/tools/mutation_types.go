package tools

const (
	writeToolName     = "write"
	editToolName      = "edit"
	multiEditToolName = "multiedit"
)

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type WritePermissionsParams struct {
	FilesystemPathMetadata
	FilePath            string `json:"file_path"`
	OldContent          string `json:"old_content,omitempty"`
	NewContent          string `json:"new_content,omitempty"`
	OldContentTruncated bool   `json:"old_content_truncated,omitempty"`
	NewContentTruncated bool   `json:"new_content_truncated,omitempty"`
	OldContentBytes     int    `json:"old_content_bytes,omitempty"`
	NewContentBytes     int    `json:"new_content_bytes,omitempty"`
}

type WriteResponseMetadata struct {
	FilesystemPathMetadata
	Diff       string `json:"diff"`
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type EditParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type EditPermissionsParams struct {
	FilesystemPathMetadata
	FilePath            string `json:"file_path"`
	OldContent          string `json:"old_content,omitempty"`
	NewContent          string `json:"new_content,omitempty"`
	OldContentTruncated bool   `json:"old_content_truncated,omitempty"`
	NewContentTruncated bool   `json:"new_content_truncated,omitempty"`
	OldContentBytes     int    `json:"old_content_bytes,omitempty"`
	NewContentBytes     int    `json:"new_content_bytes,omitempty"`
}

type EditResponseMetadata struct {
	FilesystemPathMetadata
	Diff       string `json:"diff"`
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type MultiEditParams struct {
	FilePath string          `json:"file_path"`
	Edits    []EditOperation `json:"edits"`
}

type MultiEditPermissionsParams struct {
	FilesystemPathMetadata
	FilePath            string `json:"file_path"`
	OldContent          string `json:"old_content,omitempty"`
	NewContent          string `json:"new_content,omitempty"`
	OldContentTruncated bool   `json:"old_content_truncated,omitempty"`
	NewContentTruncated bool   `json:"new_content_truncated,omitempty"`
	OldContentBytes     int    `json:"old_content_bytes,omitempty"`
	NewContentBytes     int    `json:"new_content_bytes,omitempty"`
}

type EditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type FailedEdit struct {
	Index int           `json:"index"`
	Error string        `json:"error"`
	Edit  EditOperation `json:"edit"`
}

type MultiEditResponseMetadata struct {
	FilesystemPathMetadata
	Diff         string       `json:"diff"`
	Additions    int          `json:"additions"`
	Removals     int          `json:"removals"`
	OldContent   string       `json:"old_content,omitempty"`
	NewContent   string       `json:"new_content,omitempty"`
	EditsApplied int          `json:"edits_applied"`
	EditsFailed  []FailedEdit `json:"edits_failed,omitempty"`
}

type writeExecutor struct{}
type editExecutor struct{}
type multiEditExecutor struct{}

func NewWriteExecutor() Executor     { return &writeExecutor{} }
func NewEditExecutor() Executor      { return &editExecutor{} }
func NewMultiEditExecutor() Executor { return &multiEditExecutor{} }
