package server

import "strings"

// rawSaveAllowedExt reports whether ext (with leading dot) may be written
// through the raw-save endpoint (PUT /api/files/raw). Keep in sync with the
// editable binary viewers: Excel (.xlsx), Word (.docx), PDF (.pdf).
func rawSaveAllowedExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".xlsx", ".docx", ".pdf":
		return true
	}
	return false
}
