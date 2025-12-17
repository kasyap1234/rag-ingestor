package app

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kasyap/rag-ingestor/internal/chunker"
	"github.com/kasyap/rag-ingestor/internal/cleaner"
	"github.com/kasyap/rag-ingestor/internal/extractor"
	"github.com/kasyap/rag-ingestor/internal/jobs"
	"github.com/kasyap/rag-ingestor/internal/parser"
	"github.com/labstack/echo/v4"
)

type App struct {
	router          *echo.Echo
	parser          *parser.Parser
	cleaner         *cleaner.Cleaner
	chunker         *chunker.Chunker
	semanticChunker *chunker.SemanticChunker
	extractor       *extractor.Extractor
	jobManager      *jobs.Manager
}

func New() *App {
	return &App{
		router:          echo.New(),
		parser:          parser.New(),
		cleaner:         cleaner.New(),
		chunker:         chunker.New(),
		semanticChunker: chunker.NewSemantic(),
		extractor:       extractor.New(),
		jobManager:      jobs.NewManager(4),
	}
}

func (a *App) Run() {
	a.router.Logger.Fatal(a.router.Start(":8080"))
}

func (a *App) RegisterRoutes() {
	a.router.GET("/health", a.HealthCheck)
	a.router.POST("/ingest", a.Ingest)
	a.router.POST("/ingest/batch", a.IngestBatch)
	a.router.POST("/ingest/async", a.IngestAsync)
	a.router.GET("/jobs/:id", a.GetJob)
}

// HealthCheck returns the health status of the application
func (a *App) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

// IngestResponse represents the API response for ingestion
type IngestResponse struct {
	Filename  string                     `json:"filename"`
	Content   string                     `json:"content,omitempty"`
	Chunks    []ChunkInfo                `json:"chunks,omitempty"`
	Tables    []TableInfo                `json:"tables,omitempty"`
	Metadata  IngestMetadata             `json:"metadata"`
	Extracted *extractor.DocumentMetadata `json:"extracted,omitempty"`
}

// ChunkInfo contains chunk content and metadata
type ChunkInfo struct {
	Content string            `json:"content"`
	Header  string            `json:"header,omitempty"`
	Index   int               `json:"index"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// TableInfo contains extracted table data
type TableInfo struct {
	Headers  []string   `json:"headers"`
	Rows     [][]string `json:"rows"`
	Markdown string     `json:"markdown"`
}

type IngestMetadata struct {
	OriginalSize int    `json:"original_size"`
	CleanedSize  int    `json:"cleaned_size"`
	NumChunks    int    `json:"num_chunks,omitempty"`
	NumTables    int    `json:"num_tables,omitempty"`
	FileType     string `json:"file_type"`
	ChunkMode    string `json:"chunk_mode,omitempty"`
}

// BatchResponse represents response for batch processing
type BatchResponse struct {
	TotalFiles int              `json:"total_files"`
	Processed  int              `json:"processed"`
	Failed     int              `json:"failed"`
	Results    []IngestResponse `json:"results"`
}

// AsyncResponse represents response for async job creation
type AsyncResponse struct {
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Ingest handles document ingestion for RAG
func (a *App) Ingest(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is required"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open file"})
	}
	defer src.Close()

	// Process the file
	result, err := a.processFile(file.Filename, src, c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Handle output format
	format := c.QueryParam("format")
	if format == "" {
		format = c.FormValue("format")
	}

	switch format {
	case "text", "txt":
		return c.String(http.StatusOK, result.Content)
	case "md", "markdown":
		c.Response().Header().Set("Content-Type", "text/markdown")
		return c.String(http.StatusOK, result.Content)
	default:
		return c.JSON(http.StatusOK, result)
	}
}

// IngestBatch handles batch processing of multiple files via ZIP
func (a *App) IngestBatch(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "ZIP file is required"})
	}

	if !strings.HasSuffix(strings.ToLower(file.Filename), ".zip") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file must be a ZIP archive"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open file"})
	}
	defer src.Close()

	// Read ZIP into memory
	data, err := io.ReadAll(src)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
	}

	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ZIP file"})
	}

	var results []IngestResponse
	var processed, failed int

	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Skip hidden files
		if strings.HasPrefix(f.Name, ".") || strings.Contains(f.Name, "/.") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			failed++
			continue
		}

		result, err := a.processFile(f.Name, rc, c)
		rc.Close()

		if err != nil {
			failed++
			results = append(results, IngestResponse{
				Filename: f.Name,
				Metadata: IngestMetadata{FileType: "error"},
			})
		} else {
			processed++
			results = append(results, *result)
		}
	}

	return c.JSON(http.StatusOK, BatchResponse{
		TotalFiles: processed + failed,
		Processed:  processed,
		Failed:     failed,
		Results:    results,
	})
}

// IngestAsync creates an async job for processing
func (a *App) IngestAsync(c echo.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is required"})
	}

	webhookURL := c.FormValue("webhook_url")
	
	// Create job
	job := a.jobManager.CreateJob(1, webhookURL)

	// Process in background
	go func(jobID string, filename string) {
		// Read file into memory for goroutine
		src, err := file.Open()
		if err != nil {
			a.jobManager.FailJob(jobID, "failed to open file")
			return
		}
		defer src.Close()

		data, err := io.ReadAll(src)
		if err != nil {
			a.jobManager.FailJob(jobID, "failed to read file")
			return
		}

		a.jobManager.StartProcessing(jobID)

		result, err := a.processFile(filename, bytes.NewReader(data), nil)
		if err != nil {
			a.jobManager.AddResult(jobID, jobs.JobResult{
				Filename: filename,
				Success:  false,
				Error:    err.Error(),
			})
			a.jobManager.FailJob(jobID, err.Error())
			return
		}

		a.jobManager.AddResult(jobID, jobs.JobResult{
			Filename: filename,
			Success:  true,
			Content:  result.Content,
			Data:     result,
		})
		a.jobManager.CompleteJob(jobID)

		// Call webhook if configured
		if webhookURL != "" {
			a.callWebhook(webhookURL, jobID)
		}
	}(job.ID, file.Filename)

	return c.JSON(http.StatusAccepted, AsyncResponse{
		JobID:   job.ID,
		Status:  string(job.Status),
		Message: "Job created. Poll /jobs/" + job.ID + " for status.",
	})
}

// GetJob returns job status and results
func (a *App) GetJob(c echo.Context) error {
	jobID := c.Param("id")
	
	job, ok := a.jobManager.GetJob(jobID)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "job not found"})
	}

	return c.JSON(http.StatusOK, job)
}

// processFile handles the common file processing logic
func (a *App) processFile(filename string, reader io.Reader, c echo.Context) (*IngestResponse, error) {
	// Parse the file
	parseResult, err := a.parser.Parse(filename, reader)
	if err != nil {
		return nil, err
	}

	// Clean the content
	cleanResult := a.cleaner.Clean(parseResult.Content)
	markdown := a.cleaner.ToMarkdown(cleanResult.Content)

	// Extract tables
	var tables []TableInfo
	for _, t := range parseResult.Tables {
		tables = append(tables, TableInfo{
			Headers:  t.Headers,
			Rows:     t.Rows,
			Markdown: formatTableToMarkdown(t),
		})
	}

	// Extract metadata
	extractMetadata := true
	if c != nil && c.FormValue("extract_metadata") == "false" {
		extractMetadata = false
	}
	var extracted *extractor.DocumentMetadata
	if extractMetadata {
		extracted = a.extractor.Extract(parseResult.Content)
	}

	// Handle chunking
	var chunks []ChunkInfo
	var numChunks int
	var chunkMode string

	shouldChunk := false
	if c != nil {
		shouldChunk = c.FormValue("chunk") == "true"
		chunkMode = c.FormValue("chunk_mode")
	}

	if shouldChunk {
		if chunkMode == "" {
			chunkMode = "semantic"
		}

		chunkSize := 0
		if c != nil {
			if sizeStr := c.FormValue("chunk_size"); sizeStr != "" {
				if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 {
					chunkSize = size * 4
				}
			}
		}

		if chunkMode == "semantic" {
			sc := a.semanticChunker
			if chunkSize > 0 {
				sc = chunker.NewSemanticWithConfig(chunkSize, chunkSize/10)
			}
			result := sc.Chunk(markdown)
			numChunks = result.NumChunks
			for _, ch := range result.Chunks {
				chunks = append(chunks, ChunkInfo{
					Content: ch.Content,
					Header:  ch.Header,
					Index:   ch.Index,
					Meta:    ch.Metadata,
				})
			}
		} else {
			fc := a.chunker
			if chunkSize > 0 {
				fc = chunker.NewWithConfig(chunkSize, chunkSize/8)
			}
			result := fc.Chunk(markdown)
			numChunks = result.NumChunks
			for _, ch := range result.Chunks {
				chunks = append(chunks, ChunkInfo{
					Content: ch.Content,
					Index:   ch.Index,
				})
			}
		}
	}

	return &IngestResponse{
		Filename:  filename,
		Content:   markdown,
		Chunks:    chunks,
		Tables:    tables,
		Extracted: extracted,
		Metadata: IngestMetadata{
			OriginalSize: cleanResult.OriginalSize,
			CleanedSize:  cleanResult.CleanedSize,
			NumChunks:    numChunks,
			NumTables:    len(tables),
			FileType:     parseResult.FileType,
			ChunkMode:    chunkMode,
		},
	}, nil
}

// callWebhook sends a POST to the webhook URL
func (a *App) callWebhook(url string, jobID string) {
	job, ok := a.jobManager.GetJob(jobID)
	if !ok {
		return
	}

	// Simple webhook call
	go func() {
		resp, err := http.Post(url, "application/json", strings.NewReader(`{"job_id":"`+jobID+`","status":"`+string(job.Status)+`"}`))
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}

// formatTableToMarkdown converts a table to markdown format
func formatTableToMarkdown(t parser.Table) string {
	if len(t.Headers) == 0 && len(t.Rows) == 0 {
		return ""
	}

	var result strings.Builder
	result.WriteString("|")
	for _, h := range t.Headers {
		result.WriteString(" " + h + " |")
	}
	result.WriteString("\n|")
	for range t.Headers {
		result.WriteString("---|")
	}
	result.WriteString("\n")
	for _, row := range t.Rows {
		result.WriteString("|")
		for _, cell := range row {
			result.WriteString(" " + cell + " |")
		}
		result.WriteString("\n")
	}
	return result.String()
}