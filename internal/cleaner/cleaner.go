package cleaner

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Cleaner handles text normalization and cleaning for RAG
type Cleaner struct {
	// Patterns to remove (headers, footers, page numbers, etc.)
	removePatterns []*regexp.Regexp
}

// New creates a new Cleaner with default patterns
func New() *Cleaner {
	return &Cleaner{
		removePatterns: []*regexp.Regexp{
			// Page numbers like "Page 1", "- 1 -", "1 of 10"
			regexp.MustCompile(`(?i)(?:page\s*\d+|\-\s*\d+\s*\-|\d+\s*of\s*\d+)`),
			// Common header/footer patterns
			regexp.MustCompile(`(?i)^(confidential|draft|internal use only).*$`),
			// Multiple consecutive special characters
			regexp.MustCompile(`[*#\-=_]{5,}`),
		},
	}
}

// CleanResult contains cleaned content and metadata
type CleanResult struct {
	Content      string
	OriginalSize int
	CleanedSize  int
}

// Clean processes text to make it suitable for RAG
func (c *Cleaner) Clean(text string) *CleanResult {
	originalSize := len(text)

	// Step 1: Normalize unicode (NFKC normalization)
	text = norm.NFKC.String(text)

	// Step 2: Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Step 3: Remove null bytes and control characters (except newline/tab)
	text = c.removeControlChars(text)

	// Step 4: Apply removal patterns
	for _, pattern := range c.removePatterns {
		text = pattern.ReplaceAllString(text, "")
	}

	// Step 5: Normalize whitespace
	text = c.normalizeWhitespace(text)

	// Step 6: Fix common OCR errors
	text = c.fixOCRErrors(text)

	// Step 7: Clean up empty lines (max 2 consecutive)
	text = c.limitEmptyLines(text, 2)

	// Step 8: Trim
	text = strings.TrimSpace(text)

	return &CleanResult{
		Content:      text,
		OriginalSize: originalSize,
		CleanedSize:  len(text),
	}
}

// removeControlChars removes control characters except newline and tab
func (c *Cleaner) removeControlChars(text string) string {
	var result strings.Builder
	result.Grow(len(text))

	for _, r := range text {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// normalizeWhitespace collapses multiple spaces into one
func (c *Cleaner) normalizeWhitespace(text string) string {
	// Replace tabs with spaces
	text = strings.ReplaceAll(text, "\t", " ")

	// Collapse multiple spaces into one
	spaceRegex := regexp.MustCompile(` {2,}`)
	text = spaceRegex.ReplaceAllString(text, " ")

	// Remove spaces at start/end of lines
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}

	return strings.Join(lines, "\n")
}

// fixOCRErrors corrects common OCR mistakes
func (c *Cleaner) fixOCRErrors(text string) string {
	// Fix ligatures (Unicode ligature characters)
	text = strings.ReplaceAll(text, "\uFB01", "fi") // ﬁ
	text = strings.ReplaceAll(text, "\uFB02", "fl") // ﬂ
	text = strings.ReplaceAll(text, "\uFB00", "ff") // ﬀ
	text = strings.ReplaceAll(text, "\uFB03", "ffi") // ﬃ
	text = strings.ReplaceAll(text, "\uFB04", "ffl") // ﬄ

	// Fix smart quotes
	text = strings.ReplaceAll(text, "\u2018", "'") // '
	text = strings.ReplaceAll(text, "\u2019", "'") // '
	text = strings.ReplaceAll(text, "\u201C", "\"") // "
	text = strings.ReplaceAll(text, "\u201D", "\"") // "

	// Fix dashes
	text = strings.ReplaceAll(text, "\u2014", "-") // em dash —
	text = strings.ReplaceAll(text, "\u2013", "-") // en dash –

	// Fix ellipsis
	text = strings.ReplaceAll(text, "\u2026", "...") // …

	// Fix non-breaking space
	text = strings.ReplaceAll(text, "\u00A0", " ")

	return text
}

// limitEmptyLines ensures no more than maxEmpty consecutive empty lines
func (c *Cleaner) limitEmptyLines(text string, maxEmpty int) string {
	lines := strings.Split(text, "\n")
	var result []string
	emptyCount := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			emptyCount++
			if emptyCount <= maxEmpty {
				result = append(result, line)
			}
		} else {
			emptyCount = 0
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// ToMarkdown converts cleaned text to markdown format
func (c *Cleaner) ToMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	var result strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result.WriteString("\n")
			continue
		}

		// Detect potential headings (ALL CAPS lines, short lines)
		if c.isPotentialHeading(trimmed) {
			result.WriteString("## ")
			result.WriteString(trimmed)
			result.WriteString("\n\n")
		} else {
			result.WriteString(trimmed)
			result.WriteString("\n")
		}
	}

	return result.String()
}

// isPotentialHeading detects if a line might be a heading
func (c *Cleaner) isPotentialHeading(line string) bool {
	// Short lines that are all caps might be headings
	if len(line) > 5 && len(line) < 80 {
		allUpper := true
		for _, r := range line {
			if unicode.IsLetter(r) && !unicode.IsUpper(r) {
				allUpper = false
				break
			}
		}
		return allUpper
	}
	return false
}
