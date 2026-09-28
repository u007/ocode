package session

import (
	"database/sql"
	"os"
)

// TitleForDir returns a session's persisted title for the project at wd.
//
// It is the single-row counterpart to ListRefsForDir: a dashboard that shows
// live sessions needs the title of a handful of specific ids, and paying a full
// directory scan plus index query per request (and re-scanning a project with
// thousands of legacy session files) for a handful of lookups is the wrong
// trade. This reads one indexed row from the project's index.sqlite.
//
// The bool is false when no row exists for the id — the session was never
// persisted, or has no index entry. That is not an error.
//
// DDL-free by design (openDBRaw, not openIndexDB): this runs on a read path that
// must never take the cross-process write lock or run a CREATE TABLE. Same
// discipline as StoredRevisionForDir.
func TitleForDir(wd, id string) (string, bool, error) {
	if wd == "" || id == "" {
		return "", false, nil
	}
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return "", false, err
	}
	path := indexDBPath(dir)
	if _, err := os.Stat(path); err != nil {
		// No index for this project yet: nothing is migrated, so no titles.
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	db, err := openDBRaw(path)
	if err != nil {
		return "", false, err
	}
	defer db.Close()

	var title string
	err = db.QueryRow(`SELECT title FROM sessions WHERE id = ?`, id).Scan(&title)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return title, true, nil
}
