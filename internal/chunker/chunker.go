package chunker

import (
	"strings"
	"unicode"
)

// Chunker splits text into segments suitable for RAG
type Chunker struct {
	ChunkSize    int // Target chunk size in characters
	ChunkOverlap int // Overlap between chunks
}

// New creates a new Chunker with default settings
func New() *Chunker {
	return &Chunker{
		ChunkSize:    1500, // ~375 tokens (assuming 4 chars per token)
		ChunkOverlap: 200,  // ~50 tokens overlap
	}
}

// NewWithConfig creates a Chunker with custom settings
func NewWithConfig(chunkSize, overlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 1500
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 8
	}
	return &Chunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: overlap,
	}
}

// Chunk represents a single text chunk
type Chunk struct {
	Content string
	Index   int
	Start   int // Start position in original text
	End     int // End position in original text
}

// ChunkResult contains all chunks and metadata
type ChunkResult struct {
	Chunks    []Chunk
	NumChunks int
}

// Chunk splits text into overlapping chunks
func (c *Chunker) Chunk(text string) *ChunkResult {
	if len(text) <= c.ChunkSize {
		return &ChunkResult{
			Chunks: []Chunk{
				{Content: text, Index: 0, Start: 0, End: len(text)},
			},
			NumChunks: 1,
		}
	}

	// First, split by paragraphs
	paragraphs := c.splitParagraphs(text)

	// Then combine paragraphs into chunks
	chunks := c.combineIntoChunks(paragraphs)

	return &ChunkResult{
		Chunks:    chunks,
		NumChunks: len(chunks),
	}
}

// splitParagraphs splits text into paragraphs
func (c *Chunker) splitParagraphs(text string) []string {
	// Split on double newlines (paragraph breaks)
	raw := strings.Split(text, "\n\n")

	var paragraphs []string
	for _, p := range raw {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	return paragraphs
}

// combineIntoChunks combines paragraphs into chunks of target size
func (c *Chunker) combineIntoChunks(paragraphs []string) []Chunk {
	var chunks []Chunk
	var currentChunk strings.Builder
	currentStart := 0
	position := 0
	chunkIndex := 0

	for _, para := range paragraphs {
		paraLen := len(para)

		// If single paragraph exceeds chunk size, split it
		if paraLen > c.ChunkSize {
			// Flush current chunk if not empty
			if currentChunk.Len() > 0 {
				chunks = append(chunks, Chunk{
					Content: currentChunk.String(),
					Index:   chunkIndex,
					Start:   currentStart,
					End:     position,
				})
				chunkIndex++
				currentChunk.Reset()
			}

			// Split large paragraph into sentences
			sentenceChunks := c.splitLargeParagraph(para, position, chunkIndex)
			chunks = append(chunks, sentenceChunks...)
			chunkIndex += len(sentenceChunks)
			position += paraLen + 2
			currentStart = position
			continue
		}

		// Check if adding this paragraph exceeds chunk size
		if currentChunk.Len()+paraLen+2 > c.ChunkSize {
			// Save current chunk
			if currentChunk.Len() > 0 {
				chunks = append(chunks, Chunk{
					Content: currentChunk.String(),
					Index:   chunkIndex,
					Start:   currentStart,
					End:     position,
				})
				chunkIndex++

				// Start new chunk with overlap from previous
				overlap := c.getOverlapText(currentChunk.String())
				currentChunk.Reset()
				if overlap != "" {
					currentChunk.WriteString(overlap)
					currentChunk.WriteString("\n\n")
				}
				currentStart = position - len(overlap)
			}
		}

		// Add paragraph to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
		position += paraLen + 2
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, Chunk{
			Content: currentChunk.String(),
			Index:   chunkIndex,
			Start:   currentStart,
			End:     position,
		})
	}

	return chunks
}

// splitLargeParagraph splits a paragraph that exceeds chunk size
func (c *Chunker) splitLargeParagraph(para string, startPos, startIndex int) []Chunk {
	var chunks []Chunk
	sentences := c.splitSentences(para)

	var currentChunk strings.Builder
	chunkStart := startPos
	position := startPos
	chunkIndex := startIndex

	for _, sentence := range sentences {
		sentLen := len(sentence)

		if currentChunk.Len()+sentLen+1 > c.ChunkSize && currentChunk.Len() > 0 {
			chunks = append(chunks, Chunk{
				Content: currentChunk.String(),
				Index:   chunkIndex,
				Start:   chunkStart,
				End:     position,
			})
			chunkIndex++
			currentChunk.Reset()
			chunkStart = position
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
		position += sentLen + 1
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, Chunk{
			Content: currentChunk.String(),
			Index:   chunkIndex,
			Start:   chunkStart,
			End:     position,
		})
	}

	return chunks
}

// splitSentences splits text into sentences
func (c *Chunker) splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence ending
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			// Look ahead for space or end
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
				sentences = append(sentences, strings.TrimSpace(current.String()))
				current.Reset()
			}
		}
	}

	// Don't forget remaining text
	if current.Len() > 0 {
		sentences = append(sentences, strings.TrimSpace(current.String()))
	}

	return sentences
}

// getOverlapText gets the last N characters for overlap
func (c *Chunker) getOverlapText(text string) string {
	if len(text) <= c.ChunkOverlap {
		return text
	}

	// Try to break at a sentence or paragraph boundary
	overlap := text[len(text)-c.ChunkOverlap:]

	// Find first sentence start in overlap
	if idx := strings.Index(overlap, ". "); idx != -1 && idx < len(overlap)/2 {
		overlap = overlap[idx+2:]
	}

	return strings.TrimSpace(overlap)
}
