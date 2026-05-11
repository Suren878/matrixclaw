package telegram

import "time"

const (
	defaultPollTimeout         = 30 * time.Second
	defaultPollLimit           = 100
	defaultPollRetryDelay      = 2 * time.Second
	defaultStreamFlushInterval = 800 * time.Millisecond
	defaultDaemonHTTPTimeout   = 15 * time.Second
	defaultTelegramHTTPTimeout = 45 * time.Second
	defaultButtonTextLimit     = 64
	ClientName                 = "telegram"
	defaultClientName          = ClientName
	defaultMessageLimit        = 4000

	cbPicker          = "pk:"
	cbPickerPage      = "pg:"
	cbApprovalOnce    = "ao:"
	cbApprovalSession = "as:"
	cbApprovalDeny    = "ad:"

	restartProgressText   = "Architect is restarting..."
	modelPickerPageSize   = 20
	defaultParseMode      = "HTML"
	maxTelegramImageBytes = 8 * 1024 * 1024
)
