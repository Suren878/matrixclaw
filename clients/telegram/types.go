package telegram

type Update struct {
	UpdateID           int64               `json:"update_id"`
	Message            *Message            `json:"message,omitempty"`
	GuestMessage       *Message            `json:"guest_message,omitempty"`
	InlineQuery        *InlineQuery        `json:"inline_query,omitempty"`
	ChosenInlineResult *ChosenInlineResult `json:"chosen_inline_result,omitempty"`
	CallbackQuery      *CallbackQuery      `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID          int64                 `json:"message_id"`
	Date               int64                 `json:"date,omitempty"`
	From               *User                 `json:"from,omitempty"`
	Chat               Chat                  `json:"chat"`
	GuestQueryID       string                `json:"guest_query_id,omitempty"`
	GuestBotCallerUser *User                 `json:"guest_bot_caller_user,omitempty"`
	GuestBotCallerChat *Chat                 `json:"guest_bot_caller_chat,omitempty"`
	Text               string                `json:"text,omitempty"`
	Caption            string                `json:"caption,omitempty"`
	Photo              []PhotoSize           `json:"photo,omitempty"`
	Document           *Document             `json:"document,omitempty"`
	Voice              *Voice                `json:"voice,omitempty"`
	Audio              *Audio                `json:"audio,omitempty"`
	Location           *Location             `json:"location,omitempty"`
	ReplyToMessage     *Message              `json:"reply_to_message,omitempty"`
	ReplyMarkup        *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
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

type Location struct {
	Latitude             float64 `json:"latitude"`
	Longitude            float64 `json:"longitude"`
	HorizontalAccuracy   float64 `json:"horizontal_accuracy,omitempty"`
	LivePeriod           int     `json:"live_period,omitempty"`
	Heading              int     `json:"heading,omitempty"`
	ProximityAlertRadius int     `json:"proximity_alert_radius,omitempty"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type User struct {
	ID                        int64  `json:"id"`
	IsBot                     bool   `json:"is_bot"`
	Username                  string `json:"username,omitempty"`
	CanJoinGroups             bool   `json:"can_join_groups,omitempty"`
	CanReadAllGroupMessages   bool   `json:"can_read_all_group_messages,omitempty"`
	SupportsGuestQueries      bool   `json:"supports_guest_queries,omitempty"`
	SupportsInlineQueries     bool   `json:"supports_inline_queries,omitempty"`
	CanConnectToBusiness      bool   `json:"can_connect_to_business,omitempty"`
	HasTopicsEnabled          bool   `json:"has_topics_enabled,omitempty"`
	AllowsUsersToCreateTopics bool   `json:"allows_users_to_create_topics,omitempty"`
	CanManageBots             bool   `json:"can_manage_bots,omitempty"`
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
	ChatID      int64                 `json:"chat_id"`
	Text        string                `json:"text"`
	ParseMode   string                `json:"parse_mode,omitempty"`
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type SendMessageDraftRequest struct {
	ChatID    int64  `json:"chat_id"`
	DraftID   int64  `json:"draft_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type InlineQuery struct {
	ID       string    `json:"id"`
	From     *User     `json:"from,omitempty"`
	Query    string    `json:"query,omitempty"`
	Offset   string    `json:"offset,omitempty"`
	ChatType string    `json:"chat_type,omitempty"`
	Location *Location `json:"location,omitempty"`
}

type ChosenInlineResult struct {
	ResultID        string    `json:"result_id"`
	From            *User     `json:"from,omitempty"`
	InlineMessageID string    `json:"inline_message_id,omitempty"`
	Query           string    `json:"query,omitempty"`
	Location        *Location `json:"location,omitempty"`
}

type InlineQueryResultArticle struct {
	Type                string                  `json:"type"`
	ID                  string                  `json:"id"`
	Title               string                  `json:"title"`
	Description         string                  `json:"description,omitempty"`
	InputMessageContent InputTextMessageContent `json:"input_message_content"`
	ReplyMarkup         *InlineKeyboardMarkup   `json:"reply_markup,omitempty"`
}

type InputTextMessageContent struct {
	MessageText string `json:"message_text"`
	ParseMode   string `json:"parse_mode,omitempty"`
}

type AnswerGuestQueryRequest struct {
	GuestQueryID string                   `json:"guest_query_id"`
	Result       InlineQueryResultArticle `json:"result"`
}

type SentGuestMessage struct {
	InlineMessageID string `json:"inline_message_id"`
}

type AnswerInlineQueryRequest struct {
	InlineQueryID string                     `json:"inline_query_id"`
	Results       []InlineQueryResultArticle `json:"results"`
	CacheTime     int                        `json:"cache_time,omitempty"`
	IsPersonal    bool                       `json:"is_personal,omitempty"`
}

type SendChatActionRequest struct {
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
}

type SendVoiceRequest struct {
	ChatID   int64
	Voice    []byte
	FileName string
	Caption  string
	MIMEType string
}

type SendAudioRequest struct {
	ChatID              int64
	Audio               []byte
	FileName            string
	Caption             string
	MIMEType            string
	DisableNotification bool
}

type SendDocumentRequest struct {
	ChatID   int64
	Document []byte
	FileName string
	Caption  string
	MIMEType string
}

type SentMessage struct {
	MessageID int64     `json:"message_id"`
	Voice     *Voice    `json:"voice,omitempty"`
	Audio     *Audio    `json:"audio,omitempty"`
	Document  *Document `json:"document,omitempty"`
}

type CallbackQuery struct {
	ID              string   `json:"id"`
	From            *User    `json:"from"`
	Message         *Message `json:"message,omitempty"`
	InlineMessageID string   `json:"inline_message_id,omitempty"`
	Data            string   `json:"data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

type EditMessageTextRequest struct {
	ChatID          int64                 `json:"chat_id,omitempty"`
	MessageID       int64                 `json:"message_id,omitempty"`
	InlineMessageID string                `json:"inline_message_id,omitempty"`
	Text            string                `json:"text"`
	ParseMode       string                `json:"parse_mode,omitempty"`
	ReplyMarkup     *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type EditMessageTextResponse struct {
	MessageID int64 `json:"message_id"`
}

type InputMediaAudio struct {
	Type      string `json:"type"`
	Media     string `json:"media"`
	Caption   string `json:"caption,omitempty"`
	ParseMode string `json:"parse_mode,omitempty"`
	Title     string `json:"title,omitempty"`
	Performer string `json:"performer,omitempty"`
}

type EditMessageMediaRequest struct {
	ChatID          int64                 `json:"chat_id,omitempty"`
	MessageID       int64                 `json:"message_id,omitempty"`
	InlineMessageID string                `json:"inline_message_id,omitempty"`
	Media           InputMediaAudio       `json:"media"`
	ReplyMarkup     *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type EditMessageMediaResponse struct {
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
