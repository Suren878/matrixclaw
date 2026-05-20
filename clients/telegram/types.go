package telegram

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID       int64       `json:"message_id"`
	MessageThreadID int64       `json:"message_thread_id,omitempty"`
	Date            int64       `json:"date,omitempty"`
	From            *User       `json:"from,omitempty"`
	Chat            Chat        `json:"chat"`
	Text            string      `json:"text,omitempty"`
	Caption         string      `json:"caption,omitempty"`
	Photo           []PhotoSize `json:"photo,omitempty"`
	Document        *Document   `json:"document,omitempty"`
	Voice           *Voice      `json:"voice,omitempty"`
	Audio           *Audio      `json:"audio,omitempty"`
	ReplyToMessage  *Message    `json:"reply_to_message,omitempty"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	MIMEType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

type Audio struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type GetUpdatesRequest struct {
	Offset         int64    `json:"offset,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	TimeoutSeconds int      `json:"timeout,omitempty"`
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

type GetFileRequest struct {
	FileID string `json:"file_id"`
}

type SendMessageRequest struct {
	ChatID          int64                 `json:"chat_id"`
	MessageThreadID int64                 `json:"message_thread_id,omitempty"`
	Text            string                `json:"text"`
	ParseMode       string                `json:"parse_mode,omitempty"`
	ReplyMarkup     *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type SendChatActionRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Action          string `json:"action"`
}

type SendVoiceRequest struct {
	ChatID          int64
	MessageThreadID int64
	Voice           []byte
	FileName        string
	Caption         string
	MIMEType        string
}

type SentMessage struct {
	MessageID       int64 `json:"message_id"`
	MessageThreadID int64 `json:"message_thread_id,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type EditMessageTextRequest struct {
	ChatID      int64                 `json:"chat_id"`
	MessageID   int64                 `json:"message_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type EditMessageTextResponse struct {
	MessageID int64 `json:"message_id"`
}

type AnswerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

type DeleteMessageRequest struct {
	ChatID    int64 `json:"chat_id"`
	MessageID int64 `json:"message_id"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type SetMyCommandsRequest struct {
	Commands []BotCommand     `json:"commands"`
	Scope    *BotCommandScope `json:"scope,omitempty"`
}

type DeleteMyCommandsRequest struct {
	Scope *BotCommandScope `json:"scope,omitempty"`
}

type BotCommandScope struct {
	Type   string `json:"type"`
	ChatID int64  `json:"chat_id,omitempty"`
}
