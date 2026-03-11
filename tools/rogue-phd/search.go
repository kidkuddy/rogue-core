package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Search types

type SearchOptions struct {
	Query      string
	MaxResults int
	StartYear  int
	EndYear    int
}

type PaperResult struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Authors         []string `json:"authors"`
	Abstract        string   `json:"abstract"`
	PublicationDate string   `json:"publication_date"`
	Venue           string   `json:"venue"`
	URL             string   `json:"url"`
	PDFURL          string   `json:"pdf_url"`
	DOI             string   `json:"doi"`
}

type SearchResult struct {
	Papers []PaperResult `json:"papers"`
	Total  int           `json:"total"`
}

type Searcher interface {
	Name() string
	Search(ctx context.Context, opts SearchOptions) (*SearchResult, error)
}

// --- arXiv ---

const arxivBaseURL = "http://export.arxiv.org/api/query"

type ArxivSearcher struct {
	client *http.Client
}

func NewArxivSearcher() *ArxivSearcher {
	return &ArxivSearcher{client: &http.Client{Timeout: 60 * time.Second}}
}

func (s *ArxivSearcher) Name() string { return "arxiv" }

func (s *ArxivSearcher) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}

	searchQuery := fmt.Sprintf("all:(%s)", opts.Query)
	if opts.StartYear > 0 && opts.EndYear > 0 {
		searchQuery += fmt.Sprintf(" AND submittedDate:[%d0101 TO %d1231]", opts.StartYear, opts.EndYear)
	} else if opts.StartYear > 0 {
		searchQuery += fmt.Sprintf(" AND submittedDate:[%d0101 TO 99991231]", opts.StartYear)
	} else if opts.EndYear > 0 {
		searchQuery += fmt.Sprintf(" AND submittedDate:[00000101 TO %d1231]", opts.EndYear)
	}

	params := url.Values{}
	params.Set("search_query", searchQuery)
	params.Set("start", "0")
	params.Set("max_results", fmt.Sprintf("%d", maxResults))
	params.Set("sortBy", "relevance")
	params.Set("sortOrder", "descending")

	reqURL := fmt.Sprintf("%s?%s", arxivBaseURL, params.Encode())

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.doSearch(ctx, reqURL)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "500") {
			waitTime := time.Duration(3<<attempt) * time.Second
			log.Printf("[arxiv] rate limited, waiting %v (attempt %d)", waitTime, attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}
		return nil, err
	}
	return nil, lastErr
}

func (s *ArxivSearcher) doSearch(ctx context.Context, reqURL string) (*SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("arxiv returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	papers := make([]PaperResult, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		papers = append(papers, s.entryToPaper(entry))
	}

	log.Printf("[arxiv] found %d results", len(papers))
	return &SearchResult{Papers: papers, Total: len(papers)}, nil
}

func (s *ArxivSearcher) entryToPaper(entry arxivEntry) PaperResult {
	arxivID := entry.ID
	if idx := strings.LastIndex(arxivID, "/abs/"); idx != -1 {
		arxivID = arxivID[idx+5:]
	}

	var pdfURL string
	for _, link := range entry.Links {
		if link.Title == "pdf" || strings.HasSuffix(link.Href, ".pdf") {
			pdfURL = link.Href
			break
		}
	}
	if pdfURL == "" {
		pdfURL = fmt.Sprintf("https://arxiv.org/pdf/%s.pdf", arxivID)
	}

	abstractURL := entry.ID
	if !strings.HasPrefix(abstractURL, "http") {
		abstractURL = fmt.Sprintf("https://arxiv.org/abs/%s", arxivID)
	}

	authors := make([]string, 0, len(entry.Authors))
	for _, author := range entry.Authors {
		authors = append(authors, author.Name)
	}

	pubDate := ""
	if entry.Published != "" {
		if t, err := time.Parse(time.RFC3339, entry.Published); err == nil {
			pubDate = t.Format("2006-01-02")
		}
	}

	venue := ""
	if entry.PrimaryCategory.Term != "" {
		venue = entry.PrimaryCategory.Term
	}

	abstract := strings.TrimSpace(entry.Summary)
	abstract = strings.ReplaceAll(abstract, "\n", " ")

	return PaperResult{
		ID:              arxivID,
		Title:           strings.TrimSpace(entry.Title),
		Authors:         authors,
		Abstract:        abstract,
		PublicationDate: pubDate,
		Venue:           venue,
		URL:             abstractURL,
		PDFURL:          pdfURL,
		DOI:             entry.DOI,
	}
}

type arxivFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}
type arxivEntry struct {
	ID              string        `xml:"id"`
	Title           string        `xml:"title"`
	Summary         string        `xml:"summary"`
	Published       string        `xml:"published"`
	Authors         []arxivAuthor `xml:"author"`
	Links           []arxivLink   `xml:"link"`
	DOI             string        `xml:"doi"`
	PrimaryCategory arxivCategory `xml:"primary_category"`
}
type arxivAuthor struct {
	Name string `xml:"name"`
}
type arxivLink struct {
	Href  string `xml:"href,attr"`
	Title string `xml:"title,attr"`
}
type arxivCategory struct {
	Term string `xml:"term,attr"`
}

// --- PubMed ---

const (
	pubmedSearchURL = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi"
	pubmedFetchURL  = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi"
)

type PubMedSearcher struct {
	client *http.Client
}

func NewPubMedSearcher() *PubMedSearcher {
	return &PubMedSearcher{client: &http.Client{Timeout: 60 * time.Second}}
}

func (s *PubMedSearcher) Name() string { return "pubmed" }

func (s *PubMedSearcher) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}

	terms := strings.Split(opts.Query, " OR ")
	for i, term := range terms {
		terms[i] = strings.TrimSpace(term) + "[TIAB]"
	}
	queryWithFields := strings.Join(terms, " OR ")

	if opts.StartYear > 0 && opts.EndYear > 0 {
		queryWithFields += fmt.Sprintf(" AND %d:%d[DP]", opts.StartYear, opts.EndYear)
	} else if opts.StartYear > 0 {
		queryWithFields += fmt.Sprintf(" AND %d:3000[DP]", opts.StartYear)
	} else if opts.EndYear > 0 {
		queryWithFields += fmt.Sprintf(" AND 1900:%d[DP]", opts.EndYear)
	}

	pmids, err := s.searchIDs(ctx, queryWithFields, maxResults)
	if err != nil {
		return nil, fmt.Errorf("search IDs: %w", err)
	}
	if len(pmids) == 0 {
		return &SearchResult{Papers: []PaperResult{}, Total: 0}, nil
	}

	log.Printf("[pubmed] found %d IDs", len(pmids))

	papers, err := s.fetchArticles(ctx, pmids)
	if err != nil {
		return nil, fmt.Errorf("fetch articles: %w", err)
	}

	return &SearchResult{Papers: papers, Total: len(papers)}, nil
}

func (s *PubMedSearcher) searchIDs(ctx context.Context, query string, maxResults int) ([]string, error) {
	params := url.Values{}
	params.Set("db", "pubmed")
	params.Set("term", query)
	params.Set("retmax", fmt.Sprintf("%d", maxResults))
	params.Set("retmode", "xml")
	params.Set("sort", "relevance")

	reqURL := fmt.Sprintf("%s?%s", pubmedSearchURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pubmed search returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result esearchResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse search result: %w", err)
	}
	return result.IDList.IDs, nil
}

func (s *PubMedSearcher) fetchArticles(ctx context.Context, pmids []string) ([]PaperResult, error) {
	params := url.Values{}
	params.Set("db", "pubmed")
	params.Set("id", strings.Join(pmids, ","))
	params.Set("retmode", "xml")
	params.Set("rettype", "abstract")

	reqURL := fmt.Sprintf("%s?%s", pubmedFetchURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pubmed fetch returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var articleSet pubmedArticleSet
	if err := xml.Unmarshal(body, &articleSet); err != nil {
		return nil, fmt.Errorf("parse articles: %w", err)
	}

	papers := make([]PaperResult, 0, len(articleSet.Articles))
	for _, article := range articleSet.Articles {
		papers = append(papers, s.articleToPaper(article))
	}
	return papers, nil
}

func (s *PubMedSearcher) articleToPaper(article pubmedArticle) PaperResult {
	medline := article.MedlineCitation
	pmid := medline.PMID.Text
	title := strings.TrimSpace(medline.Article.ArticleTitle)

	var authors []string
	for _, author := range medline.Article.AuthorList.Authors {
		name := ""
		if author.LastName != "" {
			name = author.LastName
			if author.ForeName != "" {
				name += ", " + author.ForeName
			} else if author.Initials != "" {
				name += " " + author.Initials
			}
		} else if author.CollectiveName != "" {
			name = author.CollectiveName
		}
		if name != "" {
			authors = append(authors, name)
		}
	}

	var abstractParts []string
	for _, text := range medline.Article.Abstract.AbstractTexts {
		if text.Label != "" {
			abstractParts = append(abstractParts, text.Label+": "+text.Text)
		} else {
			abstractParts = append(abstractParts, text.Text)
		}
	}
	abstract := strings.Join(abstractParts, " ")

	pubDate := ""
	jd := medline.Article.Journal.JournalIssue.PubDate
	if jd.Year != "" {
		pubDate = jd.Year
		if jd.Month != "" {
			pubDate += "-" + monthToNumber(jd.Month)
			if jd.Day != "" {
				pubDate += "-" + fmt.Sprintf("%02s", jd.Day)
			}
		}
	}

	venue := medline.Article.Journal.Title
	if venue == "" {
		venue = medline.Article.Journal.ISOAbbreviation
	}

	var doi string
	for _, id := range article.PubmedData.ArticleIDList.ArticleIDs {
		if id.IDType == "doi" {
			doi = id.Text
			break
		}
	}

	return PaperResult{
		ID:              pmid,
		Title:           title,
		Authors:         authors,
		Abstract:        abstract,
		PublicationDate: pubDate,
		Venue:           venue,
		URL:             fmt.Sprintf("https://pubmed.ncbi.nlm.nih.gov/%s/", pmid),
		DOI:             doi,
	}
}

func monthToNumber(month string) string {
	months := map[string]string{
		"Jan": "01", "Feb": "02", "Mar": "03", "Apr": "04",
		"May": "05", "Jun": "06", "Jul": "07", "Aug": "08",
		"Sep": "09", "Oct": "10", "Nov": "11", "Dec": "12",
	}
	if num, ok := months[month]; ok {
		return num
	}
	if len(month) <= 2 {
		return fmt.Sprintf("%02s", month)
	}
	return "01"
}

type esearchResult struct {
	XMLName xml.Name `xml:"eSearchResult"`
	Count   int      `xml:"Count"`
	IDList  struct {
		IDs []string `xml:"Id"`
	} `xml:"IdList"`
}
type pubmedArticleSet struct {
	XMLName  xml.Name        `xml:"PubmedArticleSet"`
	Articles []pubmedArticle `xml:"PubmedArticle"`
}
type pubmedArticle struct {
	MedlineCitation struct {
		PMID struct {
			Text string `xml:",chardata"`
		} `xml:"PMID"`
		Article struct {
			ArticleTitle string `xml:"ArticleTitle"`
			Abstract     struct {
				AbstractTexts []struct {
					Text  string `xml:",chardata"`
					Label string `xml:"Label,attr"`
				} `xml:"AbstractText"`
			} `xml:"Abstract"`
			AuthorList struct {
				Authors []struct {
					LastName       string `xml:"LastName"`
					ForeName       string `xml:"ForeName"`
					Initials       string `xml:"Initials"`
					CollectiveName string `xml:"CollectiveName"`
				} `xml:"Author"`
			} `xml:"AuthorList"`
			Journal struct {
				Title           string `xml:"Title"`
				ISOAbbreviation string `xml:"ISOAbbreviation"`
				JournalIssue    struct {
					PubDate struct {
						Year  string `xml:"Year"`
						Month string `xml:"Month"`
						Day   string `xml:"Day"`
					} `xml:"PubDate"`
				} `xml:"JournalIssue"`
			} `xml:"Journal"`
		} `xml:"Article"`
	} `xml:"MedlineCitation"`
	PubmedData struct {
		ArticleIDList struct {
			ArticleIDs []struct {
				IDType string `xml:"IdType,attr"`
				Text   string `xml:",chardata"`
			} `xml:"ArticleId"`
		} `xml:"ArticleIdList"`
	} `xml:"PubmedData"`
}

// --- Semantic Scholar ---

const semanticSearchURL = "https://api.semanticscholar.org/graph/v1/paper/search"

type SemanticSearcher struct {
	client *http.Client
	apiKey string
}

func NewSemanticSearcher() *SemanticSearcher {
	return &SemanticSearcher{
		client: &http.Client{Timeout: 60 * time.Second},
		apiKey: os.Getenv("SEMANTIC_SCHOLAR_API_KEY"),
	}
}

func (s *SemanticSearcher) Name() string { return "semantic" }

func (s *SemanticSearcher) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 100 {
		maxResults = 100
	}

	params := url.Values{}
	params.Set("query", opts.Query)
	params.Set("limit", fmt.Sprintf("%d", maxResults))
	params.Set("fields", "paperId,title,abstract,authors,year,venue,externalIds,url,openAccessPdf,publicationDate")

	if opts.StartYear > 0 && opts.EndYear > 0 {
		params.Set("year", fmt.Sprintf("%d-%d", opts.StartYear, opts.EndYear))
	} else if opts.StartYear > 0 {
		params.Set("year", fmt.Sprintf("%d-", opts.StartYear))
	} else if opts.EndYear > 0 {
		params.Set("year", fmt.Sprintf("-%d", opts.EndYear))
	}

	reqURL := fmt.Sprintf("%s?%s", semanticSearchURL, params.Encode())

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.doSearch(ctx, reqURL)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if strings.Contains(err.Error(), "429") {
			waitTime := time.Duration(1<<attempt) * time.Second
			log.Printf("[semantic] rate limited, waiting %v (attempt %d)", waitTime, attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}
		return nil, err
	}
	return nil, lastErr
}

func (s *SemanticSearcher) doSearch(ctx context.Context, reqURL string) (*SearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("status 429: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("semantic scholar returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp semanticResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	papers := make([]PaperResult, 0, len(apiResp.Data))
	for _, paper := range apiResp.Data {
		papers = append(papers, s.paperToPaperResult(paper))
	}

	log.Printf("[semantic] found %d results", len(papers))
	return &SearchResult{Papers: papers, Total: apiResp.Total}, nil
}

func (s *SemanticSearcher) paperToPaperResult(paper semanticPaper) PaperResult {
	authors := make([]string, 0, len(paper.Authors))
	for _, a := range paper.Authors {
		if a.Name != "" {
			authors = append(authors, a.Name)
		}
	}

	doi := paper.ExternalIDs.DOI
	pdfURL := paper.OpenAccessPDF.URL

	pubDate := paper.PublicationDate
	if pubDate == "" && paper.Year > 0 {
		pubDate = fmt.Sprintf("%d", paper.Year)
	}

	paperURL := paper.URL
	if paperURL == "" && paper.PaperID != "" {
		paperURL = fmt.Sprintf("https://www.semanticscholar.org/paper/%s", paper.PaperID)
	}

	return PaperResult{
		ID:              paper.PaperID,
		Title:           paper.Title,
		Authors:         authors,
		Abstract:        paper.Abstract,
		PublicationDate: pubDate,
		Venue:           paper.Venue,
		URL:             paperURL,
		PDFURL:          pdfURL,
		DOI:             doi,
	}
}

type semanticResponse struct {
	Total  int             `json:"total"`
	Offset int             `json:"offset"`
	Next   int             `json:"next"`
	Data   []semanticPaper `json:"data"`
}
type semanticPaper struct {
	PaperID         string `json:"paperId"`
	Title           string `json:"title"`
	Abstract        string `json:"abstract"`
	Year            int    `json:"year"`
	Venue           string `json:"venue"`
	PublicationDate string `json:"publicationDate"`
	URL             string `json:"url"`
	Authors         []struct {
		AuthorID string `json:"authorId"`
		Name     string `json:"name"`
	} `json:"authors"`
	ExternalIDs struct {
		DOI   string `json:"DOI"`
		ArXiv string `json:"ArXiv"`
	} `json:"externalIds"`
	OpenAccessPDF struct {
		URL    string `json:"url"`
		Status string `json:"status"`
	} `json:"openAccessPdf"`
}
