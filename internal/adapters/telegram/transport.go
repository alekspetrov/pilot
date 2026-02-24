package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/alekspetrov/pilot/internal/transcription"
)

// TransportConfig holds configuration for the Telegram transport layer.
type TransportConfig struct {
	AllowedIDs    []int64
	Transcription *transcription.Config
}

// Transport normalizes Telegram updates to comms.IncomingMessage
// and delegates to comms.Handler for platform-agnostic processing.
type Transport struct {
	client           *Client
	handler          *comms.Handler
	cmdHandler       *CommandHandler
	allowedIDs       map[int64]bool
	offset           int64
	transcriber      *transcription.Service
	transcriptionErr error
	stopCh           chan struct{}
	wg               sync.WaitGroup
	mu               sync.Mutex
	log              *slog.Logger
}

// NewTransport creates a new Telegram transport.
func NewTransport(client *Client, handler *comms.Handler, cmdHandler *CommandHandler, cfg *TransportConfig) *Transport {
	allowedIDs := make(map[int64]bool)
	for _, id := range cfg.AllowedIDs {
		allowedIDs[id] = true
	}

	t := &Transport{
		client:     client,
		handler:    handler,
		cmdHandler: cmdHandler,
		allowedIDs: allowedIDs,
		stopCh:     make(chan struct{}),
		log:        logging.WithComponent("telegram.transport"),
	}

	// Initialize transcription service if configured
	if cfg.Transcription != nil {
		svc, err := transcription.NewService(cfg.Transcription)
		if err != nil {
			t.transcriptionErr = err
			t.log.Warn("Transcription not available", slog.Any("error", err))
		} else {
			t.transcriber = svc
			t.log.Debug("Voice transcription enabled", slog.String("backend", svc.BackendName()))
		}
	}

	return t
}

// CheckSingleton verifies no other bot instance is already running.
func (t *Transport) CheckSingleton(ctx context.Context) error {
	return t.client.CheckSingleton(ctx)
}

// StartPolling starts polling for updates in a goroutine.
func (t *Transport) StartPolling(ctx context.Context) {
	t.wg.Add(1)
	go t.pollLoop(ctx)
}

// Stop gracefully stops the polling loop.
func (t *Transport) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

func (t *Transport) pollLoop(ctx context.Context) {
	defer t.wg.Done()
	t.log.Debug("Starting poll loop")

	for {
		select {
		case <-ctx.Done():
			t.log.Debug("Poll loop stopped")
			return
		case <-t.stopCh:
			t.log.Debug("Poll loop stopped")
			return
		default:
			t.fetchAndProcess(ctx)
		}
	}
}

func (t *Transport) fetchAndProcess(ctx context.Context) {
	updates, err := t.client.GetUpdates(ctx, t.offset, 30)
	if err != nil {
		if ctx.Err() == nil {
			t.log.Warn("Error fetching updates", slog.Any("error", err))
		}
		time.Sleep(time.Second)
		return
	}

	for _, update := range updates {
		t.processUpdate(ctx, update)
		t.mu.Lock()
		if update.UpdateID >= t.offset {
			t.offset = update.UpdateID + 1
		}
		t.mu.Unlock()
	}
}

func (t *Transport) processUpdate(ctx context.Context, update *Update) {
	// Handle callback queries (button clicks)
	if update.CallbackQuery != nil {
		t.processCallback(ctx, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	msg := update.Message
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	senderID := ""
	if msg.From != nil {
		senderID = strconv.FormatInt(msg.From.ID, 10)
	}

	// Security check
	if !t.isAllowed(msg) {
		t.log.Debug("Ignoring message from unauthorized chat/user",
			slog.String("chat_id", chatID))
		return
	}

	// Handle photo messages
	if len(msg.Photo) > 0 {
		t.processPhoto(ctx, chatID, senderID, msg)
		return
	}

	// Handle voice messages
	if msg.Voice != nil {
		t.processVoice(ctx, chatID, senderID, msg)
		return
	}

	if msg.Text == "" {
		return
	}

	text := strings.TrimSpace(msg.Text)

	// Route commands to the Telegram-specific CommandHandler
	if strings.HasPrefix(text, "/") {
		t.cmdHandler.HandleCommand(ctx, chatID, text)
		return
	}

	// Normalize to IncomingMessage and delegate to comms.Handler
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: chatID,
		SenderID:  senderID,
		Text:      text,
		RawEvent:  update,
	})
}

func (t *Transport) processCallback(ctx context.Context, callback *CallbackQuery) {
	if callback.Message == nil {
		return
	}

	chatID := strconv.FormatInt(callback.Message.Chat.ID, 10)
	senderID := ""
	if callback.From != nil {
		senderID = strconv.FormatInt(callback.From.ID, 10)
	}

	data := callback.Data

	// Handle project switch callbacks in CommandHandler
	if strings.HasPrefix(data, "switch_") {
		_ = t.client.AnswerCallback(ctx, callback.ID, "")
		projectName := strings.TrimPrefix(data, "switch_")
		t.cmdHandler.HandleCallbackSwitch(ctx, chatID, projectName)
		return
	}

	// Handle voice check status callback
	if data == "voice_check_status" {
		_ = t.client.AnswerCallback(ctx, callback.ID, "")
		t.sendVoiceSetupPrompt(ctx, chatID)
		return
	}

	// Normalize execute/cancel callbacks to comms.Handler
	actionID := data // "execute" or "cancel"
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID:  chatID,
		SenderID:   senderID,
		IsCallback: true,
		CallbackID: callback.ID,
		ActionID:   actionID,
		RawEvent:   callback,
	})
}

func (t *Transport) processPhoto(ctx context.Context, chatID, senderID string, msg *Message) {
	// Get the largest photo size (last in array)
	photo := msg.Photo[len(msg.Photo)-1]
	t.log.Debug("Received photo",
		slog.String("chat_id", chatID),
		slog.Int("width", photo.Width),
		slog.Int("height", photo.Height))

	_, _ = t.client.SendMessage(ctx, chatID, "📷 Processing image...", "")

	imagePath, err := t.downloadImage(ctx, photo.FileID)
	if err != nil {
		t.log.Warn("Failed to download image", slog.Any("error", err))
		_, _ = t.client.SendMessage(ctx, chatID, "❌ Failed to download image. Please try again.", "")
		return
	}

	prompt := msg.Caption
	if prompt == "" {
		prompt = "Analyze this image and describe what you see."
	}

	// Normalize with image path and delegate
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: chatID,
		SenderID:  senderID,
		Text:      prompt,
		ImagePath: imagePath,
		RawEvent:  msg,
	})
}

func (t *Transport) processVoice(ctx context.Context, chatID, senderID string, msg *Message) {
	if t.transcriber == nil {
		t.log.Debug("Voice message received but transcription not configured")
		voiceMsg := t.voiceNotAvailableMessage()
		_, _ = t.client.SendMessage(ctx, chatID, voiceMsg, "")
		return
	}

	voice := msg.Voice
	t.log.Debug("Received voice",
		slog.String("chat_id", chatID),
		slog.Int("duration", voice.Duration))

	_, _ = t.client.SendMessage(ctx, chatID, "🎤 Transcribing voice message...", "")

	audioPath, err := t.downloadAudio(ctx, voice.FileID)
	if err != nil {
		t.log.Warn("Failed to download voice", slog.Any("error", err))
		_, _ = t.client.SendMessage(ctx, chatID, "❌ Failed to download voice message. Please try again.", "")
		return
	}
	defer func() { _ = os.Remove(audioPath) }()

	transcribeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := t.transcriber.Transcribe(transcribeCtx, audioPath)
	if err != nil {
		t.log.Warn("Transcription failed", slog.Any("error", err))
		_, _ = t.client.SendMessage(ctx, chatID,
			"❌ Failed to transcribe voice message. Please try again or send as text.", "")
		return
	}

	if result.Text == "" {
		_, _ = t.client.SendMessage(ctx, chatID,
			"🤷 Couldn't understand the voice message. Please try again or send as text.", "")
		return
	}

	// Show transcription to user
	langInfo := ""
	if result.Language != "" && result.Language != "unknown" {
		langInfo = fmt.Sprintf(" (%s)", result.Language)
	}
	_, _ = t.client.SendMessage(ctx, chatID,
		fmt.Sprintf("🎤 Transcribed%s:\n%s", langInfo, result.Text), "")

	text := strings.TrimSpace(result.Text)

	// Route commands
	if strings.HasPrefix(text, "/") {
		t.cmdHandler.HandleCommand(ctx, chatID, text)
		return
	}

	// Normalize with voice text and delegate
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: chatID,
		SenderID:  senderID,
		Text:      text,
		VoiceText: text,
		RawEvent:  msg,
	})
}

func (t *Transport) isAllowed(msg *Message) bool {
	if len(t.allowedIDs) == 0 {
		return true
	}
	senderID := int64(0)
	if msg.From != nil {
		senderID = msg.From.ID
	}
	return t.allowedIDs[msg.Chat.ID] || t.allowedIDs[senderID]
}

// downloadAudio downloads a voice file from Telegram and saves to temp file.
func (t *Transport) downloadAudio(ctx context.Context, fileID string) (string, error) {
	file, err := t.client.GetFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("getFile failed: %w", err)
	}
	if file.FilePath == "" {
		return "", fmt.Errorf("file path not available")
	}

	data, err := t.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	ext := getFileExtension(file.FilePath, ".oga")
	return writeTempFile(data, "pilot-voice-*"+ext)
}

// downloadImage downloads an image from Telegram and saves to temp file.
func (t *Transport) downloadImage(ctx context.Context, fileID string) (string, error) {
	file, err := t.client.GetFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("getFile failed: %w", err)
	}
	if file.FilePath == "" {
		return "", fmt.Errorf("file path not available")
	}

	data, err := t.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	ext := getFileExtension(file.FilePath, ".jpg")
	return writeTempFile(data, "pilot-image-*"+ext)
}

func (t *Transport) voiceNotAvailableMessage() string {
	var sb strings.Builder
	sb.WriteString("❌ Voice transcription not available\n\n")

	if t.transcriptionErr != nil {
		errStr := t.transcriptionErr.Error()
		if strings.Contains(errStr, "no backend") || strings.Contains(errStr, "API key") {
			sb.WriteString("Missing: OpenAI API key\n\n")
			sb.WriteString("Set openai_api_key in ~/.pilot/config.yaml\n")
			sb.WriteString("Then restart bot.")
			return sb.String()
		}
	}

	sb.WriteString("To enable voice:\n")
	sb.WriteString("1. Set openai_api_key in ~/.pilot/config.yaml\n")
	sb.WriteString("2. Restart bot\n\n")
	sb.WriteString("Run 'pilot doctor' to check setup.")
	return sb.String()
}

func (t *Transport) sendVoiceSetupPrompt(ctx context.Context, chatID string) {
	status := transcription.CheckSetup(nil)

	if status.OpenAIKeySet {
		_, _ = t.client.SendMessage(ctx, chatID,
			"✅ Voice transcription is ready!\nBackend: Whisper API", "")
		return
	}

	msg := "🎤 Voice transcription not available\n\nMissing: OpenAI API key for Whisper\nSet openai_api_key in ~/.pilot/config.yaml"
	_, _ = t.client.SendMessageWithKeyboard(ctx, chatID, msg, "",
		[][]InlineKeyboardButton{
			{{Text: "🔍 Check Status", CallbackData: "voice_check_status"}},
		})
}

// TelegramMemberResolverAdapter wraps the Telegram-specific MemberResolver to
// satisfy the generic comms.MemberResolver interface.
type TelegramMemberResolverAdapter struct {
	resolver MemberResolver
}

// NewTelegramMemberResolverAdapter creates a new adapter.
func NewTelegramMemberResolverAdapter(resolver MemberResolver) *TelegramMemberResolverAdapter {
	return &TelegramMemberResolverAdapter{resolver: resolver}
}

// ResolveIdentity implements comms.MemberResolver by parsing the senderID
// as a Telegram user ID (int64) and delegating to the platform-specific resolver.
func (a *TelegramMemberResolverAdapter) ResolveIdentity(senderID string) (string, error) {
	if a.resolver == nil || senderID == "" {
		return "", nil
	}
	telegramID, err := strconv.ParseInt(senderID, 10, 64)
	if err != nil {
		return "", nil // Not a valid Telegram ID
	}
	return a.resolver.ResolveTelegramIdentity(telegramID, "")
}

// getFileExtension extracts extension from a file path, with a fallback default.
func getFileExtension(filePath, defaultExt string) string {
	for i := len(filePath) - 1; i >= 0; i-- {
		if filePath[i] == '.' {
			return filePath[i:]
		}
		if filePath[i] == '/' {
			break
		}
	}
	return defaultExt
}

// writeTempFile writes data to a temporary file and returns its path.
func writeTempFile(data []byte, pattern string) (string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tmpFile.Name(), nil
}
