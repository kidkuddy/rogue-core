package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/kidkuddy/rogue-core/core"
)

const (
	defaultDebounce    = 5 * time.Second
	telegramMaxLength  = 4096
	typingTickInterval = 4 * time.Second
)

// Option configures a Telegram source.
type Option func(*Source)

// WithDebounce sets the message debounce delay.
func WithDebounce(d time.Duration) Option {
	return func(s *Source) { s.debounce = d }
}

// WithUploadDir sets the directory for file uploads.
func WithUploadDir(dir string) Option {
	return func(s *Source) { s.uploadDir = dir }
}

// Source is a Telegram bot message source.
type Source struct {
	id        string
	agentID   string
	token     string
	debounce  time.Duration
	uploadDir string
	logger    *slog.Logger

	api     *tgbotapi.BotAPI
	botID   int64
	botUser string

	// Debounce buffers per chat
	buffers   map[int64]*chatBuffer
	buffersMu sync.Mutex
	inbound   chan<- core.Message

	// File queue: files uploaded before text are queued
	fileQueue   map[string][]queuedFile
	fileQueueMu sync.Mutex

	cancel context.CancelFunc
}

type chatBuffer struct {
	messages []bufferedMsg
	timer    *time.Timer
}

type bufferedMsg struct {
	tgMsg *tgbotapi.Message
}

type queuedFile struct {
	Path     string
	Name     string
	MimeType string
}

// New creates a new Telegram source.
func New(id, agentID, token string, logger *slog.Logger, opts ...Option) *Source {
	s := &Source{
		id:        id,
		agentID:   agentID,
		token:     token,
		debounce:  defaultDebounce,
		logger:    logger,
		buffers:   make(map[int64]*chatBuffer),
		fileQueue: make(map[string][]queuedFile),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Source) ID() string { return s.id }

func (s *Source) Start(ctx context.Context, inbound chan<- core.Message) error {
	api, err := tgbotapi.NewBotAPI(s.token)
	if err != nil {
		return fmt.Errorf("telegram bot init: %w", err)
	}

	s.api = api
	s.botID = api.Self.ID
	s.botUser = api.Self.UserName
	s.inbound = inbound

	s.logger.Info("telegram source started",
		"source_id", s.id,
		"bot", "@"+s.botUser,
		"bot_id", s.botID,
	)

	ctx, s.cancel = context.WithCancel(ctx)

	go s.pollUpdates(ctx)

	return nil
}

func (s *Source) pollUpdates(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := s.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}

			// File uploads — queue for next text message
			if update.Message.Document != nil || len(update.Message.Photo) > 0 {
				s.handleFileUpload(update.Message)
				continue
			}

			if update.Message.Text == "" {
				continue
			}

			// In groups, only respond to replies to this bot or @mentions
			if update.Message.Chat.Type == "group" || update.Message.Chat.Type == "supergroup" {
				if !s.isAddressedToBot(update.Message) {
					continue
				}
				mention := "@" + s.botUser
				update.Message.Text = strings.TrimSpace(strings.ReplaceAll(update.Message.Text, mention, ""))
			}

			s.bufferMessage(update.Message)
		}
	}
}

func (s *Source) isAddressedToBot(msg *tgbotapi.Message) bool {
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.ID == s.botID {
		return true
	}
	return strings.Contains(msg.Text, "@"+s.botUser)
}

// --- Debounce ---

func (s *Source) bufferMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	s.buffersMu.Lock()
	defer s.buffersMu.Unlock()

	buf, exists := s.buffers[chatID]
	if !exists {
		buf = &chatBuffer{}
		s.buffers[chatID] = buf
	}

	buf.messages = append(buf.messages, bufferedMsg{tgMsg: msg})

	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(s.debounce, func() {
		s.flushBuffer(chatID)
	})
}

func (s *Source) flushBuffer(chatID int64) {
	s.buffersMu.Lock()
	buf, exists := s.buffers[chatID]
	if !exists || len(buf.messages) == 0 {
		s.buffersMu.Unlock()
		return
	}

	messages := buf.messages
	buf.messages = nil
	buf.timer = nil
	delete(s.buffers, chatID)
	s.buffersMu.Unlock()

	lastMsg := messages[len(messages)-1].tgMsg

	// Combine multiple messages
	var text string
	if len(messages) == 1 {
		text = lastMsg.Text
	} else {
		parts := make([]string, len(messages))
		for i, bm := range messages {
			parts[i] = bm.tgMsg.Text
		}
		text = strings.Join(parts, "\n")
	}

	// Pop queued files
	var attachments []core.Attachment
	key := fileQueueKey(lastMsg.From.ID, chatID)
	s.fileQueueMu.Lock()
	files := s.fileQueue[key]
	delete(s.fileQueue, key)
	s.fileQueueMu.Unlock()

	for _, f := range files {
		attachments = append(attachments, core.Attachment{
			Path:     f.Path,
			Name:     f.Name,
			MimeType: f.MimeType,
		})
	}

	// Determine chat type
	chatType := "private"
	if lastMsg.Chat.Type == "group" || lastMsg.Chat.Type == "supergroup" {
		chatType = "group"
	}

	// Build metadata
	metadata := map[string]any{
		"telegram_message_id": lastMsg.MessageID,
		"telegram_chat_id":    chatID,
	}
	if lastMsg.From != nil {
		metadata["telegram_username"] = lastMsg.From.UserName
		metadata["telegram_first_name"] = lastMsg.From.FirstName
	}

	msg := core.Message{
		ID:          fmt.Sprintf("tg-%d-%d", chatID, lastMsg.MessageID),
		SourceID:    s.id,
		AgentID:     s.agentID,
		ChannelID:   fmt.Sprintf("%d", chatID),
		UserID:      fmt.Sprintf("%d", lastMsg.From.ID),
		ChatType:    chatType,
		Text:        text,
		Attachments: attachments,
		Reply:       true,
		Metadata:    metadata,
	}

	// Set reply-to if applicable
	if lastMsg.ReplyToMessage != nil {
		msg.ReplyTo = &core.MessageRef{
			ID:   fmt.Sprintf("tg-%d-%d", chatID, lastMsg.ReplyToMessage.MessageID),
			Text: lastMsg.ReplyToMessage.Text,
		}
	}

	s.inbound <- msg
}

func (s *Source) drainBuffers() {
	s.buffersMu.Lock()
	chatIDs := make([]int64, 0, len(s.buffers))
	for chatID := range s.buffers {
		chatIDs = append(chatIDs, chatID)
	}
	s.buffersMu.Unlock()

	for _, chatID := range chatIDs {
		s.flushBuffer(chatID)
	}
}

// --- File Handling ---

func (s *Source) handleFileUpload(msg *tgbotapi.Message) {
	var fileID, fileName, mimeType string

	if msg.Document != nil {
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
		mimeType = msg.Document.MimeType
	} else if len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		fileID = photo.FileID
		fileName = "photo.jpg"
		mimeType = "image/jpeg"
	}

	if fileID == "" {
		return
	}

	file, err := s.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		s.logger.Warn("failed to get file info", "error", err)
		return
	}

	fileURL := file.Link(s.token)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(fileURL)
	if err != nil {
		s.logger.Warn("failed to download file", "error", err)
		return
	}
	defer resp.Body.Close()

	uploadDir := s.uploadDir
	if uploadDir == "" {
		uploadDir = os.TempDir()
	}
	os.MkdirAll(uploadDir, 0750)

	localName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), fileName)
	localPath := filepath.Join(uploadDir, localName)

	out, err := os.Create(localPath)
	if err != nil {
		s.logger.Warn("failed to create file", "error", err)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		s.logger.Warn("failed to write file", "error", err)
		os.Remove(localPath)
		return
	}

	key := fileQueueKey(msg.From.ID, msg.Chat.ID)
	s.fileQueueMu.Lock()
	s.fileQueue[key] = append(s.fileQueue[key], queuedFile{
		Path:     localPath,
		Name:     fileName,
		MimeType: mimeType,
	})
	s.fileQueueMu.Unlock()

	s.logger.Info("file queued", "file", fileName, "user", msg.From.ID, "chat", msg.Chat.ID)

	// React with checkmark
	s.sendReaction(msg.Chat.ID, msg.MessageID, "👍")
}

func fileQueueKey(userID, chatID int64) string {
	return fmt.Sprintf("%d:%d", userID, chatID)
}

// --- Response Sending ---

func (s *Source) Send(ctx context.Context, resp core.Response) error {
	chatID, err := parseChatID(resp.ChannelID)
	if err != nil {
		return fmt.Errorf("invalid channel_id: %w", err)
	}

	replyToID := 0
	if resp.Metadata != nil {
		if id, ok := resp.Metadata["telegram_message_id"].(int); ok {
			replyToID = id
		} else if id, ok := resp.Metadata["telegram_message_id"].(float64); ok {
			replyToID = int(id)
		}
	}

	// Send attachments first
	for _, att := range resp.Attachments {
		s.sendFile(chatID, att.Path, att.Name)
	}

	// Send text (chunked if needed)
	if strings.TrimSpace(resp.Text) != "" {
		return s.sendChunked(chatID, resp.Text, replyToID)
	}

	return nil
}

func (s *Source) sendChunked(chatID int64, text string, replyToID int) error {
	chunks := SplitMessage(text)
	for i, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		msg := tgbotapi.NewMessage(chatID, chunk)
		if i == 0 && replyToID > 0 {
			msg.ReplyToMessageID = replyToID
		}
		if _, err := s.api.Send(msg); err != nil {
			s.logger.Warn("send error", "chunk", i+1, "total", len(chunks), "error", err)
			return err
		}
	}
	return nil
}

func (s *Source) sendFile(chatID int64, filePath, caption string) {
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	if caption != "" {
		doc.Caption = caption
	}
	if _, err := s.api.Send(doc); err != nil {
		s.logger.Warn("send file error", "error", err)
	}
}

func (s *Source) sendReaction(chatID int64, messageID int, emoji string) {
	params := tgbotapi.Params{}
	params.AddNonZero64("chat_id", chatID)
	params.AddNonZero("message_id", messageID)
	params.AddInterface("reaction", []map[string]string{{"type": "emoji", "emoji": emoji}})
	s.api.MakeRequest("setMessageReaction", params)
}

// SourceEnv implements core.EnvSource — exposes bot token for MCP tools.
func (s *Source) SourceEnv() map[string]string {
	return map[string]string{
		"TELEGRAM_BOT_TOKEN": s.token,
	}
}

// --- Typing Indicator ---

// StartTyping implements core.TypingSource. Accepts a string channelID.
func (s *Source) StartTyping(channelID string) func() {
	chatID, err := parseChatID(channelID)
	if err != nil {
		s.logger.Warn("typing: invalid channel_id", "channel_id", channelID, "error", err)
		return func() {}
	}
	return s.startTypingLoop(chatID)
}

// startTypingLoop sends typing indicator every 4s. Returns a stop function.
func (s *Source) startTypingLoop(chatID int64) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(typingTickInterval)
		defer ticker.Stop()
		s.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				s.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func (s *Source) Stop(ctx context.Context) error {
	s.drainBuffers()
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("telegram source stopped", "source_id", s.id)
	return nil
}

// --- Helpers ---

func parseChatID(channelID string) (int64, error) {
	var chatID int64
	_, err := fmt.Sscanf(channelID, "%d", &chatID)
	return chatID, err
}

// SplitMessage splits text at Telegram's 4096 char limit.
// Tries paragraph boundaries, then line boundaries, then hard cuts.
func SplitMessage(text string) []string {
	if len(text) <= telegramMaxLength {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= telegramMaxLength {
			chunks = append(chunks, remaining)
			break
		}

		chunk := remaining[:telegramMaxLength]

		if idx := strings.LastIndex(chunk, "\n\n"); idx > telegramMaxLength/4 {
			chunk = remaining[:idx]
			remaining = remaining[idx+2:]
		} else if idx := strings.LastIndex(chunk, "\n"); idx > telegramMaxLength/4 {
			chunk = remaining[:idx]
			remaining = remaining[idx+1:]
		} else {
			remaining = remaining[telegramMaxLength:]
		}

		chunks = append(chunks, strings.TrimSpace(chunk))
	}

	return chunks
}
