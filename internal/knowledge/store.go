package knowledge

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store provides CRUD operations on an OKF knowledge bundle.
type Store struct {
	bundle *Bundle
}

// NewStore creates a Store for the given bundle.
func NewStore(b *Bundle) *Store {
	return &Store{bundle: b}
}

// Get returns one Doc by bundle-relative path. Returns an error if the doc
// does not exist.
func (s *Store) Get(relPath string) (*Doc, error) {
	fullPath := filepath.Join(s.bundle.Root, relPath)
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: get %s: %w", relPath, err)
	}
	doc, err := ParseDoc(relPath, raw)
	if err != nil {
		return nil, fmt.Errorf("knowledge: parse %s: %w", relPath, err)
	}
	return doc, nil
}

// Search searches the bundle for docs matching a query, filtered by tags and/or
// docType. Matches against title, description, and body using case-insensitive
// substring matching.
//
// Results are sorted by relevance: title match > description match > body match.
// Pagination uses 0-based page arithmetic (page 0 = first results). Returns the
// matching docs and the total count before pagination.
func (s *Store) Search(query string, tags []string, docType string, page, pageSize int) ([]*Doc, int, error) {
	allDocs, err := s.bundle.Docs()
	if err != nil {
		return nil, 0, fmt.Errorf("knowledge: search: %w", err)
	}

	// Normalise query for case-insensitive matching.
	queryLower := strings.ToLower(query)
	hasQuery := query != ""
	hasTags := len(tags) > 0
	hasType := docType != ""

	// If no filters at all, return all docs (paginated).
	if !hasQuery && !hasTags && !hasType {
		total := len(allDocs)
		start, end := paginate(total, page, pageSize)
		return allDocs[start:end], total, nil
	}

	type scoredDoc struct {
		doc   *Doc
		score int // 3=title, 2=description, 1=body, 0=no match
	}

	var matched []scoredDoc

	for _, doc := range allDocs {
		if !doc.Conforming {
			continue
		}

		// Apply docType filter.
		if hasType && doc.Type != docType {
			continue
		}

		// Apply tags filter (doc must have ALL requested tags).
		if hasTags && !hasAllTags(doc.Tags, tags) {
			continue
		}

		// If no query, any doc that passed filters is a match (score 0).
		if !hasQuery {
			matched = append(matched, scoredDoc{doc: doc, score: 0})
			continue
		}

		// Score by where the query matches.
		score := 0
		if strings.Contains(strings.ToLower(doc.Title), queryLower) {
			score = 3
		} else if strings.Contains(strings.ToLower(doc.Description), queryLower) {
			score = 2
		} else if strings.Contains(strings.ToLower(doc.Body), queryLower) {
			score = 1
		}

		if score > 0 {
			matched = append(matched, scoredDoc{doc: doc, score: score})
		}
	}

	// Sort by score descending (higher relevance first), then by path for stable order.
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].doc.Path < matched[j].doc.Path
	})

	total := len(matched)
	start, end := paginate(total, page, pageSize)

	results := make([]*Doc, 0, end-start)
	for _, sd := range matched[start:end] {
		results = append(results, sd.doc)
	}

	return results, total, nil
}

// Write creates or updates a document in the bundle. It enforces:
//   - type is required (error if empty)
//   - path must be within the bundle root
//   - path must not be reserved (index.md, log.md)
//   - path traversal ("..") is rejected
//   - timestamp is set to the current time
//   - log.md is updated via AppendLog
//   - index.md is regenerated via GenerateIndex
//
// All operations happen under WithBundleLock.
func (s *Store) Write(relPath, docType, title, description, resource string, tags []string, body string) error {
	if docType == "" {
		return fmt.Errorf("knowledge: write %s: type is required", relPath)
	}
	if err := validateDocPath(relPath); err != nil {
		return fmt.Errorf("knowledge: write %s: %w", relPath, err)
	}

	return WithBundleLock(s.bundle.Root, func() error {
		fullPath := filepath.Join(s.bundle.Root, relPath)

		// Ensure parent directory exists.
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("knowledge: create directories for %s: %w", relPath, err)
		}

		// If the doc already exists, merge frontmatter to preserve unknown keys.
		var existingDoc *Doc
		if raw, err := os.ReadFile(fullPath); err == nil {
			existingDoc, err = ParseDoc(relPath, raw)
			if err != nil {
				slog.Debug("knowledge: error parsing existing doc for merge", "path", relPath, "err", err)
			}
		}

		now := time.Now().UTC().Truncate(time.Second)

		var doc *Doc
		if existingDoc != nil && existingDoc.Conforming {
			// Merge into existing doc: update known fields, preserve unknown keys.
			doc = existingDoc
			doc.Type = docType
			doc.Title = title
			doc.Description = description
			doc.Resource = resource
			doc.Tags = tags
			doc.Body = body
			doc.Timestamp = now
			// Status and DeprecatedReason are left unchanged (only Deprecate sets them).
		} else if existingDoc != nil && !existingDoc.Conforming {
			// Existing file has unparseable frontmatter — we cannot safely merge.
			// Create a fresh conforming doc, preserving the original body content
			// so user-authored text is not lost.
			doc = &Doc{
				Path:        relPath,
				Type:        docType,
				Title:       title,
				Description: description,
				Resource:    resource,
				Tags:        tags,
				Body:        existingDoc.Body, // preserve existing body
				Timestamp:   now,
				Conforming:  true,
			}
		} else {
			doc = &Doc{
				Path:        relPath,
				Type:        docType,
				Title:       title,
				Description: description,
				Resource:    resource,
				Tags:        tags,
				Body:        body,
				Timestamp:   now,
				Conforming:  true,
			}
		}

		rendered, err := doc.Render()
		if err != nil {
			return fmt.Errorf("knowledge: render %s: %w", relPath, err)
		}

		if err := os.WriteFile(fullPath, rendered, 0644); err != nil {
			return fmt.Errorf("knowledge: write file %s: %w", relPath, err)
		}

		// Determine action for log.
		action := "Creation"
		if existingDoc != nil {
			action = "Update"
		}

		if err := AppendLog(s.bundle, action, relPath, title); err != nil {
			slog.Error("knowledge: append log after write", "path", relPath, "err", err)
		}

		if err := GenerateIndex(s.bundle); err != nil {
			slog.Error("knowledge: regenerate index after write", "err", err)
		}

		return nil
	})
}

// Deprecate sets status to "deprecated" with the given reason on a doc.
// Updates log.md and regenerates index.md. All under WithBundleLock.
func (s *Store) Deprecate(relPath, reason string) error {
	if err := validateDocPath(relPath); err != nil {
		return fmt.Errorf("knowledge: deprecate %s: %w", relPath, err)
	}

	return WithBundleLock(s.bundle.Root, func() error {
		fullPath := filepath.Join(s.bundle.Root, relPath)

		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("knowledge: deprecate %s: %w", relPath, err)
		}

		doc, err := ParseDoc(relPath, raw)
		if err != nil {
			return fmt.Errorf("knowledge: deprecate parse %s: %w", relPath, err)
		}
		if !doc.Conforming {
			return fmt.Errorf("knowledge: deprecate %s: document is not conforming", relPath)
		}

		doc.Status = "deprecated"
		doc.DeprecatedReason = reason

		rendered, err := doc.Render()
		if err != nil {
			return fmt.Errorf("knowledge: deprecate render %s: %w", relPath, err)
		}

		if err := os.WriteFile(fullPath, rendered, 0644); err != nil {
			return fmt.Errorf("knowledge: deprecate write %s: %w", relPath, err)
		}

		summary := reason
		if title := doc.Title; title != "" {
			summary = title + ": " + reason
		}

		if err := AppendLog(s.bundle, "Deprecation", relPath, summary); err != nil {
			slog.Error("knowledge: append log after deprecation", "path", relPath, "err", err)
		}

		if err := GenerateIndex(s.bundle); err != nil {
			slog.Error("knowledge: regenerate index after deprecation", "err", err)
		}

		return nil
	})
}

// validateDocPath checks that a bundle-relative path is valid for writing:
// not reserved (index.md, log.md), and no path traversal ("..").
func validateDocPath(relPath string) error {
	// Reject path traversal.
	if strings.Contains(relPath, "..") {
		return fmt.Errorf("path contains \"..\"")
	}

	// Reject reserved files.
	base := filepath.Base(relPath)
	if base == "index.md" || base == "log.md" {
		return fmt.Errorf("path %q is reserved", relPath)
	}

	return nil
}

// hasAllTags returns true if docTags contains every tag in requiredTags.
// Comparison is case-insensitive.
func hasAllTags(docTags, requiredTags []string) bool {
	for _, rt := range requiredTags {
		found := false
		for _, dt := range docTags {
			if strings.EqualFold(dt, rt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// paginate returns start and end indices for the given page and pageSize.
// Page is 0-based. Clamps to [0, total].
func paginate(total, page, pageSize int) (int, int) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	start := page * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}
