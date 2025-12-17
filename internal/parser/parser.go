package parser

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/fumiama/go-docx"
	"github.com/ledongthuc/pdf"
	"golang.org/x/net/html"
)

// Parser handles extraction of text from various file formats
type Parser struct{}

// New creates a new Parser instance
func New() *Parser {
	return &Parser{}
}

// ParseResult contains the extracted content and metadata
type ParseResult struct {
	Content  string
	FileType string
	Tables   []Table // Extracted tables in structured format
}

// Table represents a parsed table
type Table struct {
	Headers []string
	Rows    [][]string
}

// Parse extracts text content from the given file based on its extension
func (p *Parser) Parse(filename string, reader io.Reader) (*ParseResult, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	var content string
	var tables []Table
	var err error

	switch ext {
	case ".txt", ".md":
		content, err = p.parseText(reader)
	case ".pdf":
		content, err = p.parsePDF(reader)
	case ".html", ".htm":
		content, tables, err = p.parseHTMLWithTables(reader)
	case ".docx":
		content, err = p.parseDOCX(reader)
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", ext, err)
	}

	return &ParseResult{
		Content:  content,
		FileType: ext,
		Tables:   tables,
	}, nil
}

// parseText reads plain text content
func (p *Parser) parseText(reader io.Reader) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parsePDF extracts text from PDF files
func (p *Parser) parsePDF(reader io.Reader) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	var text strings.Builder
	numPages := pdfReader.NumPage()

	for i := 1; i <= numPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		pageText, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		text.WriteString(pageText)
		text.WriteString("\n\n")
	}

	return text.String(), nil
}

// parseDOCX extracts text from Word documents
func (p *Parser) parseDOCX(reader io.Reader) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	doc, err := docx.Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to parse DOCX: %w", err)
	}

	var text strings.Builder

	for _, item := range doc.Document.Body.Items {
		switch v := item.(type) {
		case *docx.Paragraph:
			paraText := p.extractParagraphText(v)
			if paraText != "" {
				text.WriteString(paraText)
				text.WriteString("\n\n")
			}
		case *docx.Table:
			tableText := p.extractTableAsMarkdown(v)
			text.WriteString(tableText)
			text.WriteString("\n\n")
		}
	}

	return text.String(), nil
}

// extractParagraphText extracts text from a DOCX paragraph
func (p *Parser) extractParagraphText(para *docx.Paragraph) string {
	var text strings.Builder

	for _, child := range para.Children {
		if run, ok := child.(*docx.Run); ok {
			for _, runChild := range run.Children {
				if t, ok := runChild.(*docx.Text); ok {
					text.WriteString(t.Text)
				}
			}
		}
	}

	return strings.TrimSpace(text.String())
}

// extractTableAsMarkdown converts a DOCX table to markdown format
func (p *Parser) extractTableAsMarkdown(tbl *docx.Table) string {
	var rows [][]string

	for _, row := range tbl.TableRows {
		var cells []string
		for _, cell := range row.TableCells {
			cellText := ""
			for _, para := range cell.Paragraphs {
				cellText += p.extractParagraphText(para) + " "
			}
			cells = append(cells, strings.TrimSpace(cellText))
		}
		rows = append(rows, cells)
	}

	return formatMarkdownTable(rows)
}

// parseHTMLWithTables extracts text and tables from HTML
func (p *Parser) parseHTMLWithTables(reader io.Reader) (string, []Table, error) {
	doc, err := html.Parse(reader)
	if err != nil {
		return "", nil, err
	}

	var text strings.Builder
	var tables []Table
	var currentTable *Table
	var currentRow []string
	var inTable, inThead bool

	var extractContent func(*html.Node)
	extractContent = func(n *html.Node) {
		// Skip script and style tags
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}

		switch {
		case n.Type == html.ElementNode && n.Data == "table":
			inTable = true
			currentTable = &Table{}

		case n.Type == html.ElementNode && n.Data == "thead":
			inThead = true

		case n.Type == html.ElementNode && n.Data == "tr":
			currentRow = []string{}

		case n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th"):
			cellText := extractCellText(n)
			currentRow = append(currentRow, cellText)
		}

		// Process children
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractContent(c)
		}

		// Handle closing tags
		switch {
		case n.Type == html.ElementNode && n.Data == "tr":
			if currentTable != nil && len(currentRow) > 0 {
				if inThead || len(currentTable.Headers) == 0 {
					currentTable.Headers = currentRow
				} else {
					currentTable.Rows = append(currentTable.Rows, currentRow)
				}
			}

		case n.Type == html.ElementNode && n.Data == "thead":
			inThead = false

		case n.Type == html.ElementNode && n.Data == "table":
			if currentTable != nil && (len(currentTable.Headers) > 0 || len(currentTable.Rows) > 0) {
				tables = append(tables, *currentTable)
				// Add table as markdown to text output
				text.WriteString("\n")
				text.WriteString(tableToMarkdown(*currentTable))
				text.WriteString("\n")
			}
			currentTable = nil
			inTable = false

		case n.Type == html.TextNode && !inTable:
			trimmed := strings.TrimSpace(n.Data)
			if trimmed != "" {
				text.WriteString(trimmed)
				text.WriteString(" ")
			}

		case n.Type == html.ElementNode && !inTable:
			switch n.Data {
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li":
				text.WriteString("\n")
			}
		}
	}

	extractContent(doc)
	return text.String(), tables, nil
}

// extractCellText extracts all text from a table cell
func extractCellText(n *html.Node) string {
	var text strings.Builder
	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(strings.TrimSpace(node.Data))
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return strings.TrimSpace(text.String())
}

// tableToMarkdown converts a Table struct to markdown format
func tableToMarkdown(t Table) string {
	if len(t.Headers) == 0 && len(t.Rows) == 0 {
		return ""
	}

	// Use first row as headers if no explicit headers
	headers := t.Headers
	rows := t.Rows
	if len(headers) == 0 && len(rows) > 0 {
		headers = rows[0]
		rows = rows[1:]
	}

	return formatMarkdownTable(append([][]string{headers}, rows...))
}

// formatMarkdownTable formats rows as a markdown table
func formatMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	// Calculate column widths
	colWidths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	var result strings.Builder

	// Header row
	result.WriteString("|")
	for i, cell := range rows[0] {
		width := colWidths[i]
		if width < 3 {
			width = 3
		}
		result.WriteString(fmt.Sprintf(" %-*s |", width, cell))
	}
	result.WriteString("\n")

	// Separator row
	result.WriteString("|")
	for _, width := range colWidths {
		if width < 3 {
			width = 3
		}
		result.WriteString(strings.Repeat("-", width+2))
		result.WriteString("|")
	}
	result.WriteString("\n")

	// Data rows
	for _, row := range rows[1:] {
		result.WriteString("|")
		for i, cell := range row {
			width := colWidths[i]
			if width < 3 {
				width = 3
			}
			result.WriteString(fmt.Sprintf(" %-*s |", width, cell))
		}
		result.WriteString("\n")
	}

	return result.String()
}
