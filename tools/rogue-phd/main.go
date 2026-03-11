package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT,
  owner TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS topics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT,
  project_id INTEGER REFERENCES projects(id),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS papers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  external_id TEXT UNIQUE,
  title TEXT NOT NULL,
  authors TEXT,
  abstract TEXT,
  year INTEGER,
  source TEXT,
  url TEXT,
  pdf_path TEXT,
  pdf_url TEXT,
  full_text TEXT,
  fingerprint TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS searches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  topic_id INTEGER REFERENCES topics(id),
  keywords TEXT,
  databases TEXT,
  year_range TEXT,
  paper_count INTEGER,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS screening_decisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER REFERENCES papers(id),
  stage TEXT,
  decision TEXT,
  reason TEXT,
  criteria_used TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS extractions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER REFERENCES papers(id),
  category TEXT,
  content TEXT,
  citation TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS paper_topics (
  paper_id INTEGER REFERENCES papers(id),
  topic_id INTEGER REFERENCES topics(id),
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (paper_id, topic_id)
);
CREATE TABLE IF NOT EXISTS search_papers (
  search_id INTEGER REFERENCES searches(id),
  paper_id INTEGER REFERENCES papers(id),
  PRIMARY KEY (search_id, paper_id)
);
CREATE INDEX IF NOT EXISTS idx_papers_external_id ON papers(external_id);
CREATE INDEX IF NOT EXISTS idx_papers_fingerprint ON papers(fingerprint);
CREATE INDEX IF NOT EXISTS idx_screening_paper_id ON screening_decisions(paper_id);
CREATE INDEX IF NOT EXISTS idx_screening_stage ON screening_decisions(stage);
CREATE INDEX IF NOT EXISTS idx_extractions_paper_id ON extractions(paper_id);
CREATE INDEX IF NOT EXISTS idx_paper_topics_paper ON paper_topics(paper_id);
CREATE INDEX IF NOT EXISTS idx_paper_topics_topic ON paper_topics(topic_id);
CREATE INDEX IF NOT EXISTS idx_search_papers_search ON search_papers(search_id);
CREATE INDEX IF NOT EXISTS idx_search_papers_paper ON search_papers(paper_id);
CREATE INDEX IF NOT EXISTS idx_topics_project ON topics(project_id);
`

var db *sql.DB

func main() {
	if err := initDB(); err != nil {
		log.Fatalf("failed to init database: %v", err)
	}
	defer db.Close()

	s := server.NewMCPServer("rogue-phd", "1.0.0")

	s.AddTool(mcp.NewTool("search_papers",
		mcp.WithDescription("Search academic papers across arXiv, PubMed, and Semantic Scholar. Papers are deduplicated and stored in the database. Optionally link results to a topic."),
		mcp.WithString("keywords", mcp.Required(), mcp.Description("Comma-separated search keywords. Each keyword is searched independently across all databases.")),
		mcp.WithString("databases", mcp.Description("Comma-separated list of databases to search. Options: arxiv, pubmed, semantic. Default: all three.")),
		mcp.WithNumber("start_year", mcp.Description("Filter papers published from this year onward.")),
		mcp.WithNumber("end_year", mcp.Description("Filter papers published up to this year.")),
		mcp.WithNumber("max_results", mcp.Description("Maximum results per keyword per database. Default: 50, max: 200.")),
		mcp.WithNumber("topic_id", mcp.Description("Link found papers to this topic ID.")),
	), handleSearchPapers)

	s.AddTool(mcp.NewTool("download_pdf",
		mcp.WithDescription("Download a paper's PDF from its stored URL. Saves locally and attempts text extraction via pdftotext. Updates the paper record with pdf_path and full_text."),
		mcp.WithNumber("paper_id", mcp.Required(), mcp.Description("The paper ID to download the PDF for.")),
	), handleDownloadPDF)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func initDB() error {
	dataDir := os.Getenv("ROGUE_DATA")
	if dataDir == "" {
		return fmt.Errorf("ROGUE_DATA not set")
	}

	dbPath := filepath.Join(dataDir, "phd", "db", "store.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	return nil
}

func handleSearchPapers(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	keywordsStr, _ := args["keywords"].(string)
	if keywordsStr == "" {
		return mcp.NewToolResultError("keywords is required"), nil
	}

	keywords := strings.Split(keywordsStr, ",")
	for i := range keywords {
		keywords[i] = strings.TrimSpace(keywords[i])
	}

	databases := []string{"arxiv", "pubmed", "semantic"}
	if dbsStr, ok := args["databases"].(string); ok && dbsStr != "" {
		databases = nil
		for _, d := range strings.Split(dbsStr, ",") {
			databases = append(databases, strings.TrimSpace(d))
		}
	}

	startYear := 0
	if sy, ok := args["start_year"].(float64); ok {
		startYear = int(sy)
	}
	endYear := 0
	if ey, ok := args["end_year"].(float64); ok {
		endYear = int(ey)
	}
	maxResults := 50
	if mr, ok := args["max_results"].(float64); ok && mr > 0 {
		maxResults = int(mr)
	}

	var topicID int64
	if tid, ok := args["topic_id"].(float64); ok && tid > 0 {
		topicID = int64(tid)
	}

	// Build searchers
	searchers := map[string]Searcher{}
	for _, name := range databases {
		switch name {
		case "arxiv":
			searchers[name] = NewArxivSearcher()
		case "pubmed":
			searchers[name] = NewPubMedSearcher()
		case "semantic":
			searchers[name] = NewSemanticSearcher()
		}
	}

	allPapers := []PaperResult{}
	seen := map[string]bool{}

	for _, keyword := range keywords {
		for _, name := range databases {
			s, ok := searchers[name]
			if !ok {
				continue
			}

			result, err := s.Search(ctx, SearchOptions{
				Query:      keyword,
				MaxResults: maxResults,
				StartYear:  startYear,
				EndYear:    endYear,
			})
			if err != nil {
				log.Printf("[%s] search error for %q: %v", name, keyword, err)
				continue
			}

			for _, paper := range result.Papers {
				fp := computeFingerprint(paper.Title)
				if !seen[fp] {
					seen[fp] = true
					allPapers = append(allPapers, paper)
				}
			}
		}
	}

	// Store papers
	paperIDs := []int64{}
	for _, paper := range allPapers {
		id, err := storePaper(paper)
		if err != nil {
			log.Printf("store paper error: %v", err)
			continue
		}
		paperIDs = append(paperIDs, id)
	}

	// Link to topic and create search record
	if topicID > 0 && len(paperIDs) > 0 {
		yearRange := ""
		if startYear > 0 || endYear > 0 {
			yearRange = fmt.Sprintf("%d-%d", startYear, endYear)
		}
		result, err := db.Exec(`
			INSERT INTO searches (topic_id, keywords, databases, year_range, paper_count)
			VALUES (?, ?, ?, ?, ?)
		`, topicID, strings.Join(keywords, ", "), strings.Join(databases, ", "), yearRange, len(paperIDs))
		if err == nil {
			searchID, _ := result.LastInsertId()
			for _, pid := range paperIDs {
				db.Exec(`INSERT OR IGNORE INTO search_papers (search_id, paper_id) VALUES (?, ?)`, searchID, pid)
				db.Exec(`INSERT OR IGNORE INTO paper_topics (paper_id, topic_id) VALUES (?, ?)`, pid, topicID)
			}
		}
	}

	response := map[string]interface{}{
		"total_found":  len(allPapers),
		"total_stored": len(paperIDs),
		"keywords":     keywords,
		"databases":    databases,
		"paper_ids":    paperIDs,
	}

	jsonResp, _ := json.MarshalIndent(response, "", "  ")
	return mcp.NewToolResultText(string(jsonResp)), nil
}

func handleDownloadPDF(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	paperIDFloat, ok := args["paper_id"].(float64)
	if !ok {
		return mcp.NewToolResultError("paper_id is required"), nil
	}
	paperID := int64(paperIDFloat)

	// Get PDF URL
	var pdfURL string
	err := db.QueryRow(`
		SELECT COALESCE(pdf_url, CASE WHEN pdf_path LIKE 'http%' THEN pdf_path ELSE '' END, '')
		FROM papers WHERE id = ?
	`, paperID).Scan(&pdfURL)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("paper not found: %v", err)), nil
	}
	if pdfURL == "" {
		return mcp.NewToolResultError("no PDF URL available for this paper"), nil
	}

	// Download PDF
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create request: %v", err)), nil
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; academic-research-tool)")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("download failed: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("download returned status %d", resp.StatusCode)), nil
	}

	pdfData, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read body: %v", err)), nil
	}

	// Save PDF
	dataDir := os.Getenv("ROGUE_DATA")
	pdfDir := filepath.Join(dataDir, "phd", "files", "pdfs")
	os.MkdirAll(pdfDir, 0755)

	pdfPath := filepath.Join(pdfDir, fmt.Sprintf("%d.pdf", paperID))
	if err := os.WriteFile(pdfPath, pdfData, 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save failed: %v", err)), nil
	}

	// Try text extraction via pdftotext
	fullText := ""
	cmd := exec.CommandContext(ctx, "pdftotext", pdfPath, "-")
	output, err := cmd.Output()
	if err == nil {
		fullText = string(output)
	} else {
		log.Printf("pdftotext failed (paper %d): %v — agent can read PDF directly", paperID, err)
	}

	// Update DB
	db.Exec(`
		UPDATE papers SET pdf_path = ?, full_text = ?, pdf_url = COALESCE(pdf_url, ?)
		WHERE id = ?
	`, pdfPath, fullText, pdfURL, paperID)

	response := map[string]interface{}{
		"paper_id":       paperID,
		"pdf_path":       pdfPath,
		"text_extracted": len(fullText) > 0,
		"text_length":    len(fullText),
	}

	jsonResp, _ := json.MarshalIndent(response, "", "  ")
	return mcp.NewToolResultText(string(jsonResp)), nil
}

func storePaper(paper PaperResult) (int64, error) {
	fingerprint := computeFingerprint(paper.Title)
	authors := strings.Join(paper.Authors, "; ")

	year := 0
	if len(paper.PublicationDate) >= 4 {
		fmt.Sscanf(paper.PublicationDate[:4], "%d", &year)
	}

	source := "unknown"
	if strings.Contains(paper.URL, "arxiv.org") {
		source = "arxiv"
	} else if strings.Contains(paper.URL, "pubmed") {
		source = "pubmed"
	} else if strings.Contains(paper.URL, "semanticscholar.org") {
		source = "semantic"
	}

	result, err := db.Exec(`
		INSERT OR IGNORE INTO papers (external_id, title, authors, abstract, year, source, url, pdf_url, fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, paper.ID, paper.Title, authors, paper.Abstract, year, source, paper.URL, paper.PDFURL, fingerprint)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		var existingID int64
		err = db.QueryRow("SELECT id FROM papers WHERE fingerprint = ?", fingerprint).Scan(&existingID)
		if err != nil {
			return 0, err
		}
		return existingID, nil
	}

	return id, nil
}

func computeFingerprint(title string) string {
	normalized := strings.ToLower(strings.TrimSpace(title))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash)
}
