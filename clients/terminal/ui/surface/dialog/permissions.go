package dialog

import (
	"image"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
)

const PermissionsID = "permissions"

type PermissionAction string

const (
	PermissionAllow        PermissionAction = "allow"
	PermissionAllowSession PermissionAction = "allow_session"
	PermissionDeny         PermissionAction = "deny"
)

const (
	diffMaxWidth       = 180
	diffSizeRatio      = 0.8
	simpleMaxWidth     = 100
	simpleSizeRatio    = 0.6
	simpleHeightRatio  = 0.5
	layoutSpacingLines = 4
	minWindowWidth     = 77
	minWindowHeight    = 20
)

const (
	toolNameBash         = "bash"
	toolNameEdit         = "edit"
	toolNameWrite        = "write"
	toolNameMultiEdit    = "multiedit"
	toolNameDownload     = "download"
	toolNameFetch        = "fetch"
	toolNameAgenticFetch = "agentic_fetch"
	toolNameRead         = "read"
	toolNameLS           = "ls"
)

const horizontalScrollStep = 5

type permissionOption struct {
	label          string
	action         PermissionAction
	underlineIndex int
}

type Permissions struct {
	com        *surfacecommon.Common
	fullscreen bool

	permission     surfacepermission.PermissionRequest
	selectedOption int

	viewport      viewport.Model
	viewportDirty bool

	lastView     string
	lastViewRect image.Rectangle

	diffSplitMode        *bool
	defaultDiffSplitMode bool
	diffXOffset          int
	unifiedDiffContent   string
	splitDiffContent     string

	help   help.Model
	keyMap permissionsKeyMap
}

var _ Dialog = (*Permissions)(nil)

type PermissionsOption func(*Permissions)

func WithDiffMode(split bool) PermissionsOption {
	return func(p *Permissions) {
		p.diffSplitMode = &split
	}
}

func NewPermissions(com *surfacecommon.Common, perm surfacepermission.PermissionRequest, opts ...PermissionsOption) *Permissions {
	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()

	km := defaultPermissionsKeyMap()
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.KeyMap = viewport.KeyMap{
		Up:           km.ScrollUp,
		Down:         km.ScrollDown,
		Left:         km.ScrollLeft,
		Right:        km.ScrollRight,
		PageUp:       key.NewBinding(key.WithDisabled()),
		PageDown:     key.NewBinding(key.WithDisabled()),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
	}

	p := &Permissions{
		com:            com,
		permission:     perm,
		selectedOption: 0,
		viewport:       vp,
		help:           h,
		keyMap:         km,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func (p *Permissions) calculateContentWidth(width int) int {
	t := p.com.Styles
	const dialogHorizontalPadding = 2
	return width - t.Dialog.View.GetHorizontalFrameSize() - dialogHorizontalPadding
}

func (*Permissions) ID() string {
	return PermissionsID
}

func (p *Permissions) Permission() surfacepermission.PermissionRequest {
	return p.permission
}
