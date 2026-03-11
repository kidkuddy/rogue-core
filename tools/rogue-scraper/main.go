package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("rogue-scraper", "1.0.0")

	s.AddTool(mcp.NewTool("scrape",
		mcp.WithDescription("Load a URL in headless Chrome and extract page content. Returns the text content, or targeted content via CSS selectors. Use for JS-rendered pages that curl can't handle."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The URL to scrape")),
		mcp.WithString("selector", mcp.Description("CSS selector to extract specific elements. If omitted, returns full page text.")),
		mcp.WithNumber("wait_seconds", mcp.Description("Seconds to wait for page to render before extracting. Default: 3")),
		mcp.WithBoolean("screenshot", mcp.Description("Take a screenshot instead of extracting text. Returns the file path to the saved PNG.")),
	), handleScrape)

	s.AddTool(mcp.NewTool("scrape_multi",
		mcp.WithDescription("Scrape multiple URLs in sequence using headless Chrome. Returns results for each URL."),
		mcp.WithString("urls", mcp.Required(), mcp.Description("Comma-separated list of URLs to scrape")),
		mcp.WithString("selector", mcp.Description("CSS selector to extract specific elements from each page")),
		mcp.WithNumber("wait_seconds", mcp.Description("Seconds to wait per page. Default: 3")),
	), handleScrapeMulti)

	s.AddTool(mcp.NewTool("scrape_extract",
		mcp.WithDescription("Scrape a page and extract structured data from repeating elements (e.g., job listings, search results). Returns JSON array of extracted items."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The URL to scrape")),
		mcp.WithString("item_selector", mcp.Required(), mcp.Description("CSS selector for each repeating item container (e.g., '.job-card', 'article.listing')")),
		mcp.WithString("fields", mcp.Required(), mcp.Description("JSON object mapping field names to CSS selectors within each item. Example: {\"title\": \"h3\", \"company\": \".company-name\", \"link\": \"a@href\"} — use @attr to extract an attribute instead of text.")),
		mcp.WithNumber("wait_seconds", mcp.Description("Seconds to wait for page to render. Default: 3")),
		mcp.WithNumber("max_items", mcp.Description("Maximum number of items to extract. Default: 50")),
	), handleScrapeExtract)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func newAllocator(ctx context.Context) (context.Context, context.CancelFunc) {
	return chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.UserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		)...,
	)
}

func handleScrape(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	url, _ := args["url"].(string)
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	selector, _ := args["selector"].(string)
	waitSec := 3.0
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = w
	}
	takeScreenshot, _ := args["screenshot"].(bool)

	allocCtx, allocCancel := newAllocator(ctx)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, time.Duration(waitSec+30)*time.Second)
	defer timeoutCancel()

	if takeScreenshot {
		var buf []byte
		err := chromedp.Run(taskCtx,
			chromedp.Navigate(url),
			chromedp.Sleep(time.Duration(waitSec)*time.Second),
			chromedp.FullScreenshot(&buf, 90),
		)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("screenshot failed: %v", err)), nil
		}

		dataDir := os.Getenv("ROGUE_DATA")
		if dataDir == "" {
			dataDir = "/tmp"
		}
		screenshotDir := filepath.Join(dataDir, "screenshots")
		os.MkdirAll(screenshotDir, 0750)
		filename := fmt.Sprintf("screenshot_%d.png", time.Now().UnixNano())
		path := filepath.Join(screenshotDir, filename)
		if err := os.WriteFile(path, buf, 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save screenshot: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Screenshot saved: %s", path)), nil
	}

	var content string
	var actions []chromedp.Action
	actions = append(actions,
		chromedp.Navigate(url),
		chromedp.Sleep(time.Duration(waitSec)*time.Second),
	)

	if selector != "" {
		actions = append(actions, chromedp.TextContent(selector, &content, chromedp.ByQueryAll))
	} else {
		actions = append(actions, chromedp.TextContent("body", &content, chromedp.ByQuery))
	}

	if err := chromedp.Run(taskCtx, actions...); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("scrape failed: %v", err)), nil
	}

	if len(content) > 50000 {
		content = content[:50000] + "\n\n[truncated — content exceeded 50000 chars]"
	}

	return mcp.NewToolResultText(content), nil
}

func handleScrapeMulti(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	urlsStr, _ := args["urls"].(string)
	if urlsStr == "" {
		return mcp.NewToolResultError("urls is required"), nil
	}

	selector, _ := args["selector"].(string)
	waitSec := 3.0
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = w
	}

	urls := strings.Split(urlsStr, ",")
	for i := range urls {
		urls[i] = strings.TrimSpace(urls[i])
	}

	allocCtx, allocCancel := newAllocator(ctx)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	type result struct {
		URL     string `json:"url"`
		Content string `json:"content,omitempty"`
		Error   string `json:"error,omitempty"`
	}

	var results []result
	for _, u := range urls {
		if u == "" {
			continue
		}

		pageCtx, pageCancel := context.WithTimeout(taskCtx, time.Duration(waitSec+30)*time.Second)

		var content string
		var actions []chromedp.Action
		actions = append(actions,
			chromedp.Navigate(u),
			chromedp.Sleep(time.Duration(waitSec)*time.Second),
		)
		if selector != "" {
			actions = append(actions, chromedp.TextContent(selector, &content, chromedp.ByQueryAll))
		} else {
			actions = append(actions, chromedp.TextContent("body", &content, chromedp.ByQuery))
		}

		if err := chromedp.Run(pageCtx, actions...); err != nil {
			results = append(results, result{URL: u, Error: err.Error()})
		} else {
			if len(content) > 20000 {
				content = content[:20000] + "\n[truncated]"
			}
			results = append(results, result{URL: u, Content: content})
		}
		pageCancel()
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

func handleScrapeExtract(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	url, _ := args["url"].(string)
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	itemSelector, _ := args["item_selector"].(string)
	if itemSelector == "" {
		return mcp.NewToolResultError("item_selector is required"), nil
	}

	fieldsStr, _ := args["fields"].(string)
	if fieldsStr == "" {
		return mcp.NewToolResultError("fields is required"), nil
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(fieldsStr), &fields); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid fields JSON: %v", err)), nil
	}

	waitSec := 3.0
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = w
	}
	maxItems := 50
	if m, ok := args["max_items"].(float64); ok && m > 0 {
		maxItems = int(m)
	}

	allocCtx, allocCancel := newAllocator(ctx)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, time.Duration(waitSec+30)*time.Second)
	defer timeoutCancel()

	jsFields, _ := json.Marshal(fields)
	js := fmt.Sprintf(`
	(function() {
		const items = document.querySelectorAll(%q);
		const fields = %s;
		const maxItems = %d;
		const results = [];
		for (let i = 0; i < Math.min(items.length, maxItems); i++) {
			const item = items[i];
			const record = {};
			for (const [name, sel] of Object.entries(fields)) {
				let actualSel = sel;
				let attr = null;
				if (sel.includes('@')) {
					const parts = sel.split('@');
					actualSel = parts[0];
					attr = parts[1];
				}
				const el = actualSel ? item.querySelector(actualSel) : item;
				if (el) {
					if (attr) {
						record[name] = el.getAttribute(attr) || '';
					} else {
						record[name] = el.textContent.trim();
					}
				} else {
					record[name] = '';
				}
			}
			results.push(record);
		}
		return JSON.stringify(results);
	})()
	`, itemSelector, string(jsFields), maxItems)

	var resultJSON string
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(url),
		chromedp.Sleep(time.Duration(waitSec)*time.Second),
		chromedp.Evaluate(js, &resultJSON),
	)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("extraction failed: %v", err)), nil
	}

	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(resultJSON), &items); err != nil {
		return mcp.NewToolResultText(resultJSON), nil
	}

	out, _ := json.MarshalIndent(items, "", "  ")
	return mcp.NewToolResultText(fmt.Sprintf("Extracted %d items:\n\n%s", len(items), string(out))), nil
}
