package telegram

import "time"

const (
	pollTimeout            = 30 * time.Second
	pollLimit              = 100
	pollRetryDelay         = 2 * time.Second
	streamFlushInterval    = 800 * time.Millisecond
	chatActionInterval     = 4 * time.Second
	daemonHTTPTimeout      = 15 * time.Second
	telegramHTTPTimeout    = 45 * time.Second
	defaultButtonTextLimit = 64
	ClientName             = "telegram"
	defaultMessageLimit    = 4000
	maxCallbackDataBytes   = 64

	cbPicker          = "pk:"
	cbPickerPage      = "pg:"
	cbCallbackRef     = "rf:"
	cbApprovalOnce    = "ao:"
	cbApprovalSession = "as:"
	cbApprovalGlobal  = "ag:"
	cbApprovalDeny    = "ad:"
	cbApprovalReason  = "ar:"

	restartProgressText   = "Architect is restarting..."
	modelPickerPageSize   = 20
	defaultParseMode      = "HTML"
	maxTelegramImageBytes = 8 * 1024 * 1024
)
