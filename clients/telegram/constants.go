package telegram

import "time"

const (
	defaultPollTimeout         = 30 * time.Second
	defaultPollLimit           = 100
	defaultPollRetryDelay      = 2 * time.Second
	defaultStreamFlushInterval = 800 * time.Millisecond
	defaultDraftRefresh        = 20 * time.Second
	defaultDaemonHTTPTimeout   = 15 * time.Second
	defaultTelegramHTTPTimeout = 45 * time.Second
	defaultButtonTextLimit     = 64
	ClientName                 = "telegram"
	defaultClientName          = ClientName
	defaultMessageLimit        = 4000
	maxCallbackDataBytes       = 64

	cbPicker          = "pk:"
	cbPickerPage      = "pg:"
	cbCallbackRef     = "rf:"
	cbApprovalOnce    = "ao:"
	cbApprovalSession = "as:"
	cbApprovalDeny    = "ad:"

	restartProgressText      = "Architect is restarting..."
	defaultThinkingDraftText = "✍️"
	modelPickerPageSize      = 20
	defaultParseMode         = "HTML"
	maxTelegramImageBytes    = 8 * 1024 * 1024
)
