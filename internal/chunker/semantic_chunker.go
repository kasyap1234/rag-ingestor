package chunker

import (
	"regexp"
	"strings"
)

// SemanticChunker splits text by document structure (headers, sections)
type SemanticChunker struct {
	MaxChunkSize int // Maximum size per chunk
	MinChunkSize int // Minimum size to create a separate chunk
}

// NewSemantic creates a semantic chunker with default settings
func NewSemantic() *SemanticChunker {
	return &SemanticChunker{
		MaxChunkSize: 2000,
		MinChunkSize: 100,
	}
}

// NewSemanticWithConfig creates a semantic chunker with custom settings
func NewSemanticWithConfig(maxSize, minSize int) *SemanticChunker {
	if maxSize <= 0 {
		maxSize = 2000
	}
	if minSize <= 0 {
		minSize = 100
	}
	return &SemanticChunker{
		MaxChunkSize: maxSize,
		MinChunkSize: minSize,
	}
}

// Section represents a document section with its header and content
type Section struct {
	Header  string
	Level   int // 1 for #, 2 for ##, etc.
	Content string
}

// SemanticChunkResult contains chunks organized by document structure
type SemanticChunkResult struct {
	Chunks    []SemanticChunk
	NumChunks int
}

// SemanticChunk represents a semantically meaningful chunk
type SemanticChunk struct {
	Content  string
	Header   string
	Level    int
	Index    int
	Metadata map[string]string
}

// headerPattern matches markdown headers
var headerPattern = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// Chunk splits text by semantic structure (headers/sections)
func (s *SemanticChunker) Chunk(text string) *SemanticChunkResult {
	sections := s.splitBySections(text)

	if len(sections) == 0 {
		// No headers found, fall back to single chunk
		return &SemanticChunkResult{
			Chunks: []SemanticChunk{
				{Content: strings.TrimSpace(text), Index: 0},
			},
			NumChunks: 1,
		}
	}

	chunks := s.sectionsToChunks(sections)

	return &SemanticChunkResult{
		Chunks:    chunks,
		NumChunks: len(chunks),
	}
}

// splitBySections splits text into sections based on headers
func (s *SemanticChunker) splitBySections(text string) []Section {
	matches := headerPattern.FindAllStringSubmatchIndex(text, -1)

	if len(matches) == 0 {
		return nil
	}

	var sections []Section

	for i, match := range matches {
		headerStart := match[0]
		headerEnd := match[1]
		levelStart := match[2]
		levelEnd := match[3]
		titleStart := match[4]
		titleEnd := match[5]

		level := levelEnd - levelStart // Number of # characters
		title := text[titleStart:titleEnd]

		// Get content until next header or end of text
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		} else {
			contentEnd = len(text)
		}

		content := strings.TrimSpace(text[headerEnd:contentEnd])

		sections = append(sections, Section{
			Header:  title,
			Level:   level,
			Content: content,
		})

		// Also capture any text before the first header
		if i == 0 && headerStart > 0 {
			preamble := strings.TrimSpace(text[:headerStart])
			if len(preamble) > 0 {
				// Insert preamble at the beginning
				sections = append([]Section{{
					Header:  "",
					Level:   0,
					Content: preamble,
				}}, sections...)
			}
		}
	}

	return sections
}

// sectionsToChunks converts sections to chunks, respecting max size
func (s *SemanticChunker) sectionsToChunks(sections []Section) []SemanticChunk {
	var chunks []SemanticChunk
	var currentChunk strings.Builder
	var currentHeader string
	var currentLevel int
	chunkIndex := 0

	flushChunk := func() {
		content := strings.TrimSpace(currentChunk.String())
		if len(content) >= s.MinChunkSize {
			chunks = append(chunks, SemanticChunk{
				Content: content,
				Header:  currentHeader,
				Level:   currentLevel,
				Index:   chunkIndex,
				Metadata: map[string]string{
					"header": currentHeader,
				},
			})
			chunkIndex++
		}
		currentChunk.Reset()
	}

	for _, section := range sections {
		sectionText := s.formatSection(section)

		// If adding this section exceeds max size, flush current chunk
		if currentChunk.Len()+len(sectionText) > s.MaxChunkSize && currentChunk.Len() > 0 {
			flushChunk()
		}

		// If section itself is too large, split it
		if len(sectionText) > s.MaxChunkSize {
			// Flush any existing content first
			if currentChunk.Len() > 0 {
				flushChunk()
			}

			// Split large section into smaller chunks
			subChunks := s.splitLargeSection(section, chunkIndex)
			chunks = append(chunks, subChunks...)
			chunkIndex += len(subChunks)
			continue
		}

		// Start new chunk with this section's header if empty
		if currentChunk.Len() == 0 {
			currentHeader = section.Header
			currentLevel = section.Level
		}

		currentChunk.WriteString(sectionText)
		currentChunk.WriteString("\n\n")
	}

	// Flush remaining content
	if currentChunk.Len() > 0 {
		flushChunk()
	}

	return chunks
}

// formatSection formats a section with its header
func (s *SemanticChunker) formatSection(section Section) string {
	if section.Header == "" {
		return section.Content
	}

	headerPrefix := strings.Repeat("#", section.Level)
	return headerPrefix + " " + section.Header + "\n\n" + section.Content
}

// splitLargeSection splits a section that exceeds max chunk size
func (s *SemanticChunker) splitLargeSection(section Section, startIndex int) []SemanticChunk {
	var chunks []SemanticChunk
	content := section.Content
	headerPrefix := ""
	if section.Header != "" {
		headerPrefix = strings.Repeat("#", section.Level) + " " + section.Header + "\n\n"
	}

	// Split by paragraphs
	paragraphs := strings.Split(content, "\n\n")
	var currentChunk strings.Builder
	currentChunk.WriteString(headerPrefix)

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if currentChunk.Len()+len(para) > s.MaxChunkSize && currentChunk.Len() > len(headerPrefix) {
			chunks = append(chunks, SemanticChunk{
				Content: strings.TrimSpace(currentChunk.String()),
				Header:  section.Header,
				Level:   section.Level,
				Index:   startIndex + len(chunks),
				Metadata: map[string]string{
					"header": section.Header,
					"part":   "continued",
				},
			})
			currentChunk.Reset()
			currentChunk.WriteString(headerPrefix)
		}

		currentChunk.WriteString(para)
		currentChunk.WriteString("\n\n")
	}

	if currentChunk.Len() > len(headerPrefix) {
		chunks = append(chunks, SemanticChunk{
			Content: strings.TrimSpace(currentChunk.String()),
			Header:  section.Header,
			Level:   section.Level,
			Index:   startIndex + len(chunks),
			Metadata: map[string]string{
				"header": section.Header,
			},
		})
	}

	return chunks
}
