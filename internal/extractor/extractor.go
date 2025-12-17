package extractor

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DocumentMetadata contains extracted structured data
type DocumentMetadata struct {
	DocumentType string            `json:"document_type,omitempty"` // "invoice", "receipt", "contract", etc.
	Invoice      *InvoiceMetadata  `json:"invoice,omitempty"`
	Dates        []ExtractedDate   `json:"dates,omitempty"`
	Amounts      []ExtractedAmount `json:"amounts,omitempty"`
	Emails       []string          `json:"emails,omitempty"`
	PhoneNumbers []string          `json:"phone_numbers,omitempty"`
	Addresses    []string          `json:"addresses,omitempty"`
}

// InvoiceMetadata contains invoice-specific fields
type InvoiceMetadata struct {
	InvoiceNumber string  `json:"invoice_number,omitempty"`
	OrderNumber   string  `json:"order_number,omitempty"`
	Date          string  `json:"date,omitempty"`
	DueDate       string  `json:"due_date,omitempty"`
	Subtotal      float64 `json:"subtotal,omitempty"`
	Tax           float64 `json:"tax,omitempty"`
	Total         float64 `json:"total,omitempty"`
	Currency      string  `json:"currency,omitempty"`
	Vendor        string  `json:"vendor,omitempty"`
	Customer      string  `json:"customer,omitempty"`
	LineItems     []LineItem `json:"line_items,omitempty"`
}

// LineItem represents a single line item in an invoice
type LineItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity,omitempty"`
	UnitPrice   float64 `json:"unit_price,omitempty"`
	Amount      float64 `json:"amount"`
}

// ExtractedDate represents a date found in the document
type ExtractedDate struct {
	Original string `json:"original"`
	Parsed   string `json:"parsed,omitempty"` // ISO format
	Context  string `json:"context,omitempty"`
}

// ExtractedAmount represents a monetary amount found in the document
type ExtractedAmount struct {
	Original string  `json:"original"`
	Value    float64 `json:"value"`
	Currency string  `json:"currency,omitempty"`
	Context  string  `json:"context,omitempty"`
}

// Extractor handles metadata extraction from text
type Extractor struct {
	patterns map[string]*regexp.Regexp
}

// New creates a new Extractor with compiled patterns
func New() *Extractor {
	return &Extractor{
		patterns: compilePatterns(),
	}
}

// compilePatterns creates all regex patterns used for extraction
func compilePatterns() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{
		// Invoice patterns
		"invoice_number": regexp.MustCompile(`(?i)(?:invoice|inv)[\s#:.-]*([A-Z0-9]+-?[A-Z0-9]+)`),
		"order_number":   regexp.MustCompile(`(?i)(?:order|po)[\s#:.-]*([A-Z0-9]+-?[A-Z0-9]+)`),
		
		// Date patterns
		"date_mdy":    regexp.MustCompile(`\b(\d{1,2})[/\-](\d{1,2})[/\-](\d{2,4})\b`),
		"date_dmy":    regexp.MustCompile(`\b(\d{1,2})\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]*\s+(\d{4})\b`),
		"date_ymd":    regexp.MustCompile(`\b(\d{4})[/\-](\d{1,2})[/\-](\d{1,2})\b`),
		"date_label":  regexp.MustCompile(`(?i)(invoice\s*date|date|due\s*date|payment\s*date)[\s:]+([^\n]+)`),
		
		// Amount patterns
		"amount_dollar": regexp.MustCompile(`\$\s*([\d,]+\.?\d*)`),
		"amount_euro":   regexp.MustCompile(`€\s*([\d,]+\.?\d*)`),
		"amount_pound":  regexp.MustCompile(`£\s*([\d,]+\.?\d*)`),
		"amount_label":  regexp.MustCompile(`(?i)(total|subtotal|sub\s*total|tax|amount\s*due|balance)[\s:]*\$?\s*([\d,]+\.?\d*)`),
		
		// Contact patterns
		"email": regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`),
		"phone": regexp.MustCompile(`(?:\+?1[-.\s]?)?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`),
	}
}

// Extract extracts metadata from text content
func (e *Extractor) Extract(text string) *DocumentMetadata {
	meta := &DocumentMetadata{}
	
	// Detect document type
	meta.DocumentType = e.detectDocumentType(text)
	
	// Extract emails and phone numbers
	meta.Emails = e.extractEmails(text)
	meta.PhoneNumbers = e.extractPhoneNumbers(text)
	
	// Extract dates
	meta.Dates = e.extractDates(text)
	
	// Extract amounts
	meta.Amounts = e.extractAmounts(text)
	
	// If it's an invoice, extract invoice-specific fields
	if meta.DocumentType == "invoice" {
		meta.Invoice = e.extractInvoiceMetadata(text)
	}
	
	return meta
}

// detectDocumentType determines the type of document
func (e *Extractor) detectDocumentType(text string) string {
	textLower := strings.ToLower(text)
	
	switch {
	case strings.Contains(textLower, "invoice"):
		return "invoice"
	case strings.Contains(textLower, "receipt"):
		return "receipt"
	case strings.Contains(textLower, "contract") || strings.Contains(textLower, "agreement"):
		return "contract"
	case strings.Contains(textLower, "quotation") || strings.Contains(textLower, "quote"):
		return "quote"
	default:
		return "document"
	}
}

// extractEmails finds all email addresses in text
func (e *Extractor) extractEmails(text string) []string {
	matches := e.patterns["email"].FindAllString(text, -1)
	return uniqueStrings(matches)
}

// extractPhoneNumbers finds all phone numbers in text
func (e *Extractor) extractPhoneNumbers(text string) []string {
	matches := e.patterns["phone"].FindAllString(text, -1)
	return uniqueStrings(matches)
}

// extractDates finds and parses dates in text
func (e *Extractor) extractDates(text string) []ExtractedDate {
	var dates []ExtractedDate
	seen := make(map[string]bool)
	
	// Find labeled dates first
	labelMatches := e.patterns["date_label"].FindAllStringSubmatch(text, -1)
	for _, match := range labelMatches {
		if len(match) >= 3 {
			dateStr := strings.TrimSpace(match[2])
			if !seen[dateStr] && len(dateStr) > 4 && len(dateStr) < 50 {
				parsed := e.parseDate(dateStr)
				dates = append(dates, ExtractedDate{
					Original: dateStr,
					Parsed:   parsed,
					Context:  strings.TrimSpace(match[1]),
				})
				seen[dateStr] = true
			}
		}
	}
	
	return dates
}

// parseDate attempts to parse a date string to ISO format
func (e *Extractor) parseDate(dateStr string) string {
	formats := []string{
		"January 2, 2006",
		"Jan 2, 2006",
		"2006-01-02",
		"01/02/2006",
		"02/01/2006",
		"1/2/2006",
		"2/1/2006",
	}
	
	dateStr = strings.TrimSpace(dateStr)
	
	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t.Format("2006-01-02")
		}
	}
	
	return ""
}

// extractAmounts finds monetary amounts in text
func (e *Extractor) extractAmounts(text string) []ExtractedAmount {
	var amounts []ExtractedAmount
	
	// Dollar amounts
	for _, match := range e.patterns["amount_dollar"].FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			value := parseAmount(match[1])
			if value > 0 {
				amounts = append(amounts, ExtractedAmount{
					Original: match[0],
					Value:    value,
					Currency: "USD",
				})
			}
		}
	}
	
	// Euro amounts
	for _, match := range e.patterns["amount_euro"].FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			value := parseAmount(match[1])
			if value > 0 {
				amounts = append(amounts, ExtractedAmount{
					Original: match[0],
					Value:    value,
					Currency: "EUR",
				})
			}
		}
	}
	
	// Pound amounts
	for _, match := range e.patterns["amount_pound"].FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			value := parseAmount(match[1])
			if value > 0 {
				amounts = append(amounts, ExtractedAmount{
					Original: match[0],
					Value:    value,
					Currency: "GBP",
				})
			}
		}
	}
	
	return amounts
}

// extractInvoiceMetadata extracts invoice-specific fields
func (e *Extractor) extractInvoiceMetadata(text string) *InvoiceMetadata {
	inv := &InvoiceMetadata{}
	
	// Invoice number
	if match := e.patterns["invoice_number"].FindStringSubmatch(text); len(match) >= 2 {
		inv.InvoiceNumber = match[1]
	}
	
	// Order number
	if match := e.patterns["order_number"].FindStringSubmatch(text); len(match) >= 2 {
		inv.OrderNumber = match[1]
	}
	
	// Extract labeled amounts
	for _, match := range e.patterns["amount_label"].FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			label := strings.ToLower(match[1])
			value := parseAmount(match[2])
			
			switch {
			case strings.Contains(label, "subtotal") || strings.Contains(label, "sub total"):
				inv.Subtotal = value
			case strings.Contains(label, "tax"):
				inv.Tax = value
			case strings.Contains(label, "total") || strings.Contains(label, "amount") || strings.Contains(label, "balance"):
				if inv.Total == 0 || value > inv.Total {
					inv.Total = value
				}
			}
		}
	}
	
	// Set currency based on detected amounts
	if match := e.patterns["amount_dollar"].FindString(text); match != "" {
		inv.Currency = "USD"
	} else if match := e.patterns["amount_euro"].FindString(text); match != "" {
		inv.Currency = "EUR"
	} else if match := e.patterns["amount_pound"].FindString(text); match != "" {
		inv.Currency = "GBP"
	}
	
	return inv
}

// parseAmount converts a string amount to float64
func parseAmount(s string) float64 {
	// Remove commas and spaces
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return value
}

// uniqueStrings removes duplicates from a string slice
func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
