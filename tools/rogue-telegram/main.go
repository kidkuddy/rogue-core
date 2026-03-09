package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
		mcp.WithDescription("Send a new message to a chat. Use for proactive messages, not replies."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Message text (supports Markdown)")),
		mcp.WithString("chat_id", mcp.Description("Chat ID. Defaults to the current channel.")),
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
	if chatID == "" {
		chatID = os.Getenv("ROGUE_CHANNEL_ID")
	}
	if chatID == "" {
		return mcp.NewToolResultError("chat_id is required (or set ROGUE_CHANNEL_ID)"), nil
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	params := url.Values{
		"chat_id": {chatID},
		"text":    {text},
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
