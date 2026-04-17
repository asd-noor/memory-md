// Package engine implements all read and write operations on the memory store.
//
// It is the single place that touches both the SQLite index and the markdown
// files. The watcher calls HandleChanged / HandleDeleted; the daemon calls the
// higher-level methods (Get, Search, New, Update, Delete, CreateFile,
// DeleteFile).
package engine

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"memory-md/internal/parser"
	"memory-md/internal/rrf"
	"memory-md/sidecar"
)

// Engine wires the SQLite database, sidecar client, and markdown directory.
type Engine struct {
	db             *sql.DB
	memDir         string
	sidecarCl      *sidecar.Client // nil if sidecar sock path unknown; graceful no-op
	activeIndexing int32           // atomic counter: >0 means indexing is in progress
}

// New constructs an Engine. sidecarSockPath may be empty if the sidecar is not used.
func New(db *sql.DB, memDir, sidecarSockPath string) *Engine {
	var cl *sidecar.Client
	if sidecarSockPath != "" {
		cl = sidecar.New(sidecarSockPath)
	}
	return &Engine{db: db, memDir: memDir, sidecarCl: cl}
}

// SidecarActive reports whether the embedding sidecar is configured and
// presumed running (i.e. it started successfully at daemon launch).
func (e *Engine) SidecarActive() bool {
	return e.sidecarCl != nil
}

// IsIndexing reports whether any indexing operation is currently in progress.
func (e *Engine) IsIndexing() bool {
	return atomic.LoadInt32(&e.activeIndexing) > 0
}

// MemDir returns the memory directory this engine is operating on.
func (e *Engine) MemDir() string {
	return e.memDir
}

// ── Read operations ───────────────────────────────────────────────────────────

// GetResult is the return type of Get.
type GetResult struct {
	Heading string
	Content string
}

// Get performs an exact path lookup.
func (e *Engine) Get(path string) (*GetResult, error) {
	var heading, content string
	err := e.db.QueryRow(
		`SELECT heading, content FROM sections WHERE path = ?`, path,
	).Scan(&heading, &content)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("section not found: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("engine.Get: %w", err)
	}
	return &GetResult{Heading: heading, Content: content}, nil
}

// SearchResult is one item in a Search response.
type SearchResult struct {
	Path    string
	Heading string
	Content string
}

// Search runs hybrid FTS5 + vector search fused with RRF.
// Falls back to FTS5-only when the sidecar is not running.
func (e *Engine) Search(query string, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	fetch := topK * 5

	// FTS5 leg.
	ftsRows, err := e.db.Query(
		`SELECT rowid FROM sections_fts WHERE sections_fts MATCH ? ORDER BY rank LIMIT ?`,
		query, fetch,
	)
	if err != nil {
		return nil, fmt.Errorf("engine.Search FTS5: %w", err)
	}
	var ftsRowids []int64
	for ftsRows.Next() {
		var id int64
		if err := ftsRows.Scan(&id); err != nil {
			ftsRows.Close()
			return nil, err
		}
		ftsRowids = append(ftsRowids, id)
	}
	ftsRows.Close()

	// Vector leg (only when sidecar is available).
	var vecRowids []int64
	if e.sidecarCl != nil {
		vec, err := e.sidecarCl.EmbedOne(query)
		if err == nil && vec != nil {
			blob, err := sqlite_vec.SerializeFloat32(vec)
			if err == nil {
				vRows, err := e.db.Query(
					`SELECT rowid FROM sections_vec WHERE embedding MATCH ? AND k=? ORDER BY distance`,
					blob, fetch,
				)
				if err == nil {
					for vRows.Next() {
						var id int64
						if err := vRows.Scan(&id); err != nil {
							vRows.Close()
							break
						}
						vecRowids = append(vecRowids, id)
					}
					vRows.Close()
				}
			}
		}
	}

	// Fuse with RRF.
	fused := rrf.Merge(ftsRowids, vecRowids)
	if len(fused) > topK {
		fused = fused[:topK]
	}

	results := make([]SearchResult, 0, len(fused))
	for _, r := range fused {
		var path, heading, content string
		err := e.db.QueryRow(
			`SELECT path, heading, content FROM sections WHERE rowid = ?`, r.Rowid,
		).Scan(&path, &heading, &content)
		if err != nil {
			continue
		}
		results = append(results, SearchResult{Path: path, Heading: heading, Content: content})
	}
	return results, nil
}

// ListFiles returns the names (without .md) of all indexed files, sorted alphabetically.
func (e *Engine) ListFiles() ([]string, error) {
	rows, err := e.db.Query(`SELECT file_path FROM files ORDER BY file_path`)
	if err != nil {
		return nil, fmt.Errorf("engine.ListFiles: %w", err)
	}
	defer rows.Close()

	prefix := e.memDir + string(filepath.Separator)
	var names []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			continue
		}
		name := strings.TrimPrefix(fp, prefix)
		name = strings.TrimSuffix(name, ".md")
		names = append(names, name)
	}
	return names, rows.Err()
}

// ListSections returns all section paths within a named file, sorted alphabetically.
func (e *Engine) ListSections(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	filePath := filepath.Join(e.memDir, name+".md")

	// Check the file exists in the index.
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM files WHERE file_path = ?`, filePath).Scan(&count); err != nil {
		return nil, fmt.Errorf("engine.ListSections: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("file not found: %s", name)
	}

	rows, err := e.db.Query(
		`SELECT path FROM sections WHERE file_path = ? ORDER BY heading_start_byte`,
		filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("engine.ListSections: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// ── File-level operations ─────────────────────────────────────────────────────

// ValidateName checks that a file name (without .md) is safe.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("name must not start with '.'")
	}
	if strings.HasSuffix(name, ".md") {
		return fmt.Errorf("name must not include .md suffix")
	}
	return nil
}

// CreateFile creates an empty .md file. Fails if it already exists.
func (e *Engine) CreateFile(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	path := filepath.Join(e.memDir, name+".md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("file already exists: %s", name)
		}
		return fmt.Errorf("engine.CreateFile: %w", err)
	}
	return f.Close()
}

// DeleteFile removes a .md file and all associated index data.
func (e *Engine) DeleteFile(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	filePath := filepath.Join(e.memDir, name+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", name)
	}

	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("engine.DeleteFile: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM sections_vec WHERE rowid IN (SELECT rowid FROM sections WHERE file_path = ?)`,
		filePath,
	); err != nil {
		return fmt.Errorf("engine.DeleteFile vec: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE file_path = ?`, filePath); err != nil {
		return fmt.Errorf("engine.DeleteFile files: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("engine.DeleteFile commit: %w", err)
	}

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("engine.DeleteFile remove: %w", err)
	}
	return nil
}

// ── Section write operations ──────────────────────────────────────────────────

// New creates a new section by appending or inserting into the .md file.
// headingOverride may be empty (defaults to last path segment).
func (e *Engine) New(path, headingOverride, content string) error {
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return fmt.Errorf("path must have at least two segments (file/section): %s", path)
	}
	name := segments[0]
	filePath := filepath.Join(e.memDir, name+".md")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", name)
	}

	// Check section does not already exist.
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM sections WHERE path = ?`, path).Scan(&count); err != nil {
		return fmt.Errorf("engine.New check existing: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("section already exists: %s", path)
	}

	level := len(segments)
	heading := headingOverride
	if heading == "" {
		heading = segments[len(segments)-1]
	}
	hashes := strings.Repeat("#", level)
	block := fmt.Sprintf("\n%s %s\n\n%s\n", hashes, heading, content)

	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("engine.New read: %w", err)
	}

	var newSrc []byte
	if level == 2 {
		// Top-level section: append to EOF.
		newSrc = append(src, []byte(block)...)
	} else {
		// Nested: insert at parent's EndByte.
		parentPath := strings.Join(segments[:len(segments)-1], "/")
		var parentEnd int64
		err := e.db.QueryRow(
			`SELECT end_byte FROM sections WHERE path = ?`, parentPath,
		).Scan(&parentEnd)
		if err == sql.ErrNoRows {
			return fmt.Errorf("parent section not found: %s", parentPath)
		}
		if err != nil {
			return fmt.Errorf("engine.New parent lookup: %w", err)
		}
		ins := []byte(block)
		newSrc = append(src[:parentEnd:parentEnd], append(ins, src[parentEnd:]...)...)
	}

	return atomicWrite(filePath, newSrc)
}

// Update replaces the immediate body of an existing section.
func (e *Engine) Update(path, content string) error {
	var startByte, endByte int64
	var level int
	var filePath string
	err := e.db.QueryRow(
		`SELECT file_path, start_byte, end_byte, level FROM sections WHERE path = ?`, path,
	).Scan(&filePath, &startByte, &endByte, &level)
	if err == sql.ErrNoRows {
		return fmt.Errorf("section not found: %s", path)
	}
	if err != nil {
		return fmt.Errorf("engine.Update lookup: %w", err)
	}

	// Body end: start of first direct child heading, or end_byte if none.
	var bodyEnd int64
	err = e.db.QueryRow(
		`SELECT MIN(heading_start_byte) FROM sections
		 WHERE file_path = ? AND level = ? AND path LIKE ?`,
		filePath, level+1, path+"/%",
	).Scan(&bodyEnd)
	if err != nil || bodyEnd == 0 {
		bodyEnd = endByte
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("engine.Update read: %w", err)
	}

	newSrc := make([]byte, 0, int64(len(src))+(int64(len(content))+1)-(bodyEnd-startByte))
	newSrc = append(newSrc, src[:startByte]...)
	newSrc = append(newSrc, []byte(content)...)
	newSrc = append(newSrc, '\n')
	newSrc = append(newSrc, src[bodyEnd:]...)

	return atomicWrite(filePath, newSrc)
}

// Delete removes a section and all its children from the .md file.
func (e *Engine) Delete(path string) error {
	var filePath string
	var headingStart, endByte int64
	err := e.db.QueryRow(
		`SELECT file_path, heading_start_byte, end_byte FROM sections WHERE path = ?`, path,
	).Scan(&filePath, &headingStart, &endByte)
	if err == sql.ErrNoRows {
		return fmt.Errorf("section not found: %s", path)
	}
	if err != nil {
		return fmt.Errorf("engine.Delete lookup: %w", err)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("engine.Delete read: %w", err)
	}

	newSrc := append(src[:headingStart:headingStart], src[endByte:]...)
	return atomicWrite(filePath, newSrc)
}

// ── Watcher callbacks ─────────────────────────────────────────────────────────

// HandleChanged re-indexes a file that was created or modified.
func (e *Engine) HandleChanged(filePath string) error {
	atomic.AddInt32(&e.activeIndexing, 1)
	defer atomic.AddInt32(&e.activeIndexing, -1)

	src, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // race: file gone before we read it
		}
		return fmt.Errorf("engine.HandleChanged read: %w", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("engine.HandleChanged stat: %w", err)
	}
	mtime := info.ModTime().UnixNano()

	result := parser.Parse(filePath, src)

	// Batch-embed all sections (nil when sidecar absent).
	var blobs [][]byte
	if e.sidecarCl != nil && len(result.Sections) > 0 {
		texts := make([]string, len(result.Sections))
		for i, s := range result.Sections {
			texts[i] = s.Path + " " + s.HeadingTxt + " " + s.Content
		}
		vecs, err := e.sidecarCl.Embed(texts)
		if err == nil && vecs != nil {
			blobs = make([][]byte, len(vecs))
			for i, v := range vecs {
				if b, err := sqlite_vec.SerializeFloat32(v); err == nil {
					blobs[i] = b
				}
			}
		}
	}

	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("engine.HandleChanged begin tx: %w", err)
	}
	defer tx.Rollback()

	// Clear old data.
	if _, err := tx.Exec(
		`DELETE FROM sections_vec WHERE rowid IN (SELECT rowid FROM sections WHERE file_path = ?)`,
		filePath,
	); err != nil {
		return fmt.Errorf("engine.HandleChanged del vec: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE file_path = ?`, filePath); err != nil {
		return fmt.Errorf("engine.HandleChanged del files: %w", err)
	}

	// Insert file metadata.
	if _, err := tx.Exec(
		`INSERT INTO files (file_path, file_mtime, title, description) VALUES (?, ?, ?, ?)`,
		filePath, mtime, nullStr(result.Title), nullStr(result.Description),
	); err != nil {
		return fmt.Errorf("engine.HandleChanged insert files: %w", err)
	}

	for i, s := range result.Sections {
		res, err := tx.Exec(
			`INSERT INTO sections (id, path, file_path, heading_start_byte, start_byte, end_byte, level, heading, content)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Path, s.FilePath, s.HeadingStartByte, s.StartByte, s.EndByte,
			s.Level, s.HeadingTxt, s.Content,
		)
		if err != nil {
			return fmt.Errorf("engine.HandleChanged insert section %s: %w", s.Path, err)
		}
		if blobs != nil && i < len(blobs) && blobs[i] != nil {
			rowid, _ := res.LastInsertId()
			if _, err := tx.Exec(
				`INSERT INTO sections_vec(rowid, embedding, section_id) VALUES (?, ?, ?)`,
				rowid, blobs[i], s.ID,
			); err != nil {
				return fmt.Errorf("engine.HandleChanged insert vec %s: %w", s.Path, err)
			}
		}
	}

	return tx.Commit()
}

// HandleDeleted removes index data for a deleted file.
func (e *Engine) HandleDeleted(filePath string) error {
	atomic.AddInt32(&e.activeIndexing, 1)
	defer atomic.AddInt32(&e.activeIndexing, -1)

	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("engine.HandleDeleted begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM sections_vec WHERE rowid IN (SELECT rowid FROM sections WHERE file_path = ?)`,
		filePath,
	); err != nil {
		return fmt.Errorf("engine.HandleDeleted del vec: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE file_path = ?`, filePath); err != nil {
		return fmt.Errorf("engine.HandleDeleted del files: %w", err)
	}
	return tx.Commit()
}

// ── Startup walk ──────────────────────────────────────────────────────────────

// SyncDir walks memDir (root level only) and updates the index incrementally.
// Files whose mtime matches the cached value are skipped. Files no longer on
// disk are removed from the index.
func (e *Engine) SyncDir() error {
	atomic.AddInt32(&e.activeIndexing, 1)
	defer atomic.AddInt32(&e.activeIndexing, -1)

	entries, err := os.ReadDir(e.memDir)
	if err != nil {
		return fmt.Errorf("engine.SyncDir readdir: %w", err)
	}

	walked := make(map[string]struct{})
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		filePath := filepath.Join(e.memDir, de.Name())
		walked[filePath] = struct{}{}

		info, err := de.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime().UnixNano()

		var cachedMtime int64
		err = e.db.QueryRow(
			`SELECT file_mtime FROM files WHERE file_path = ?`, filePath,
		).Scan(&cachedMtime)
		if err == nil && cachedMtime == mtime {
			continue // unchanged
		}
		if err := e.HandleChanged(filePath); err != nil {
			fmt.Fprintf(os.Stderr, "memory-md: sync %s: %v\n", filePath, err)
		}
	}

	// Remove stale entries.
	rows, err := e.db.Query(`SELECT file_path FROM files`)
	if err != nil {
		return fmt.Errorf("engine.SyncDir list files: %w", err)
	}
	var stale []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err == nil {
			if _, ok := walked[fp]; !ok {
				stale = append(stale, fp)
			}
		}
	}
	rows.Close()

	for _, fp := range stale {
		if err := e.HandleDeleted(fp); err != nil {
			fmt.Fprintf(os.Stderr, "memory-md: remove stale %s: %v\n", fp, err)
		}
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".memory-md-tmp-*")
	if err != nil {
		return fmt.Errorf("atomicWrite create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("atomicWrite write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicWrite close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomicWrite rename: %w", err)
	}
	return nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Ensure time import is used (for potential future mtime comparisons).
var _ = time.Now
