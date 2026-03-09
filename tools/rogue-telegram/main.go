package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	s := server.NewMCPServer("rogue-telegram", "1.0.0")

	s.AddTool(mcp.NewTool("react",
		mcp.WithDescription("React to a message with an emoji. Uses the current message by default."),
		mcp.WithString("emoji", mcp.Required(), mcp.Description("Emoji to react with (e.g. 👍, 🔥, ❤️, 👀, 🤔)")),
		mcp.WithString("message_id", mcp.Description("Message ID to react to. Defaults to the current message.")),
		mcp.WithString("chat_id", mcp.Description("Chat ID. Defaults to the current channel.")),
	), handleReact)

	s.AddTool(mcp.NewTool("send_message",
		mcp.WithDescription("Send a message to a chat or user. Provide either chat_id (direct) or user_id (resolved from known channels)."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Message text (supports Markdown)")),
		mcp.WithString("chat_id", mcp.Description("Telegram chat ID. Defaults to the current channel.")),
		mcp.WithString("user_id", mcp.Description("User ID to send to. Resolves their private chat on the current source. Use this instead of chat_id when targeting a user.")),
	), handleSendMessage)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func handleReact(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	emoji, _ := args["emoji"].(string)
	if emoji == "" {
		return mcp.NewToolResultError("emoji is required"), nil
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return mcp.NewToolResultError("not available: no bot token"), nil
	}

	chatID, _ := args["chat_id"].(string)
	if chatID == "" {
		chatID = os.Getenv("ROGUE_CHANNEL_ID")
	}
	messageID, _ := args["message_id"].(string)
	if messageID == "" {
		messageID = os.Getenv("ROGUE_MESSAGE_ID")
	}

	if chatID == "" || messageID == "" {
		return mcp.NewToolResultError("chat_id and message_id are required (or set ROGUE_CHANNEL_ID/ROGUE_MESSAGE_ID)"), nil
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMessageReaction", token)
	body := fmt.Sprintf(`{"chat_id":%s,"message_id":%s,"reaction":[{"type":"emoji","emoji":"%s"}]}`, chatID, messageID, emoji)

	resp, err := http.Post(apiURL, "application/json", strings.NewReader(body))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("telegram API error: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return mcp.NewToolResultError(fmt.Sprintf("telegram API returned %d", resp.StatusCode)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Reacted with %s", emoji)), nil
}

func handleSendMessage(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	text, _ := args["text"].(string)
	if text == "" {
		return mcp.NewToolResultError("text is required"), nil
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return mcp.NewToolResultError("not available: no bot token"), nil
	}

	chatID, _ := args["chat_id"].(string)
	userID, _ := args["user_id"].(string)

	// Resolve user_id → chat_id via user_channels
	if chatID == "" && userID != "" {
		resolved, err := resolveUserChannel(userID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("can't resolve user: %v", err)), nil
		}
		chatID = resolved
	}

	if chatID == "" {
		chatID = os.Getenv("ROGUE_CHANNEL_ID")
	}
	if chatID == "" {
		return mcp.NewToolResultError("chat_id or user_id is required"), nil
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	params := url.Values{
		"chat_id":    {chatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
	}

	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("telegram API error: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return mcp.NewToolResultError(fmt.Sprintf("telegram API returned %d", resp.StatusCode)), nil
	}

	return mcp.NewToolResultText("Message sent."), nil
}

// resolveUserChannel looks up a user's private channel_id from the IAM database.
// It queries user_channels for the user on the current source, preferring private chats.
func resolveUserChannel(userID string) (string, error) {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		return "", fmt.Errorf("ROGUE_DATA not set")
	}

	sourceID := os.Getenv("ROGUE_SOURCE_ID")
	if sourceID == "" {
		return "", fmt.Errorf("ROGUE_SOURCE_ID not set")
	}

	dbPath := filepath.Join(dataDir, "iam", "db", "store.sqlite")
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return "", fmt.Errorf("open IAM db: %w", err)
	}
	defer db.Close()

	// Prefer private chat, fall back to any chat type
	var channelID string
	err = db.QueryRow(`SELECT channel_id FROM user_channels
		WHERE user_id = ? AND source_id = ?
		ORDER BY CASE WHEN chat_type = 'private' THEN 0 ELSE 1 END, last_seen DESC
		LIMIT 1`, userID, sourceID).Scan(&channelID)
	if err != nil {
		return "", fmt.Errorf("no known channel for user %s on %s", userID, sourceID)
	}

	return channelID, nil
}
