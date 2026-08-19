package main

import (
	"context"
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/semaphore"
	_ "modernc.org/sqlite"
)

type DBEntry struct {
	Path       string
	ParentPath string
	Name       string
	Size       int64
	IsDir      bool
}

type ItemInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

type FolderView struct {
	Path     string     `json:"path"`
	Name     string     `json:"name"`
	Size     int64      `json:"size"`
	Children []ItemInfo `json:"children"`
	Parent   string     `json:"parent"`
}

type FileStat struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type App struct {
	ctx      context.Context
	db       *sql.DB
	dbPath   string
	rootPath string
	dbMutex  sync.RWMutex
}

func NewApp() *App {
	return &App{
		dbPath: "outaspace.db",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.initDB(); err != nil {
		println("Error initializing SQLite:", err.Error())
	}
}

func (a *App) shutdown(ctx context.Context) {
	a.closeAndWipeDB()
}

func (a *App) initDB() error {
	a.dbMutex.Lock()
	defer a.dbMutex.Unlock()

	db, err := sql.Open("sqlite", a.dbPath)
	if err != nil {
		return err
	}

	a.db = db

	// High throughput PRAGMAs
	pragmas := []string{
		"PRAGMA synchronous = OFF;",
		"PRAGMA journal_mode = MEMORY;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -64000;", // 64 MB cache
	}
	for _, p := range pragmas {
		if _, err := a.db.Exec(p); err != nil {
			return err
		}
	}

	return a.wipeTables()
}

func (a *App) wipeTables() error {
	schema := `
	DROP TABLE IF EXISTS entries;
	CREATE TABLE entries (
		path TEXT PRIMARY KEY,
		parent_path TEXT,
		name TEXT,
		size INTEGER DEFAULT 0,
		is_dir INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_parent ON entries(parent_path);
	`
	_, err := a.db.Exec(schema)
	return err
}

func (a *App) closeAndWipeDB() {
	a.dbMutex.Lock()
	defer a.dbMutex.Unlock()

	if a.db != nil {
		_ = a.wipeTables()
		_ = a.db.Close()
		a.db = nil
	}

	// Delete sqlite file on disk upon exit
	_ = os.Remove(a.dbPath)
	_ = os.Remove(a.dbPath + "-wal")
	_ = os.Remove(a.dbPath + "-shm")
}

func (a *App) SelectDirectory(speed string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Directory to Scan",
	})
	if err != nil || dir == "" {
		return ""
	}

	go a.scanDirectory(dir, speed)
	return dir
}

// GetRootFolderView queries SQLite for the root directory view
func (a *App) GetRootFolderView() *FolderView {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()
	return a.queryFolderView(a.rootPath)
}

// GetFolderView queries SQLite for a specific subdirectory view
func (a *App) GetFolderView(path string) *FolderView {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()
	return a.queryFolderView(path)
}

func (a *App) queryFolderView(path string) *FolderView {
	if a.db == nil || path == "" {
		return nil
	}

	var rootName string
	var totalSize int64
	err := a.db.QueryRow("SELECT name, size FROM entries WHERE path = ?", path).Scan(&rootName, &totalSize)
	if err != nil {
		rootName = filepath.Base(path)
	}

	view := &FolderView{
		Path:     path,
		Name:     rootName,
		Size:     totalSize,
		Children: make([]ItemInfo, 0),
	}

	if path != a.rootPath {
		view.Parent = filepath.Dir(path)
	}

	rows, err := a.db.Query(`
		SELECT name, path, size, is_dir 
		FROM entries 
		WHERE parent_path = ? 
		ORDER BY size DESC
	`, path)
	if err != nil {
		return view
	}
	defer rows.Close()

	for rows.Next() {
		var item ItemInfo
		var isDirInt int
		if err := rows.Scan(&item.Name, &item.Path, &item.Size, &isDirInt); err == nil {
			item.IsDir = (isDirInt == 1)
			view.Children = append(view.Children, item)
		}
	}

	return view
}

func (a *App) scanDirectory(root string, speed string) {
	numCPU := goruntime.NumCPU()
	var maxWorkers int
	switch speed {
	case "slow":
		maxWorkers = 1
	case "medium":
		maxWorkers = numCPU / 2
		if maxWorkers < 1 {
			maxWorkers = 1
		}
	case "fast":
		fallthrough
	default:
		maxWorkers = numCPU
	}

	sem := semaphore.NewWeighted(int64(maxWorkers))

	// Re-verify clean state in SQLite before scan
	a.dbMutex.Lock()
	_ = a.wipeTables()
	a.rootPath = root
	a.dbMutex.Unlock()

	fileChan := make(chan FileStat, 5000)
	dbChan := make(chan DBEntry, 10000)
	var wg sync.WaitGroup

	// Background worker 1: consume fileChan and emit batches to frontend for rain animation
	go func() {
		ticker := time.NewTicker(32 * time.Millisecond)
		defer ticker.Stop()
		var batch []FileStat

		for {
			select {
			case f, ok := <-fileChan:
				if !ok {
					if len(batch) > 0 {
						runtime.EventsEmit(a.ctx, "files-scanned", batch)
					}
					return
				}
				batch = append(batch, f)
				if len(batch) >= 100 {
					runtime.EventsEmit(a.ctx, "files-scanned", batch)
					batch = batch[:0]
				}
			case <-ticker.C:
				if len(batch) > 0 {
					runtime.EventsEmit(a.ctx, "files-scanned", batch)
					batch = batch[:0]
				}
			}
		}
	}()

	// Background worker 2: batch inserts into SQLite
	var dbWg sync.WaitGroup
	dbWg.Add(1)
	go func() {
		defer dbWg.Done()

		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		var batch []DBEntry

		flush := func() {
			if len(batch) == 0 {
				return
			}
			a.dbMutex.Lock()
			if a.db != nil {
				tx, err := a.db.Begin()
				if err == nil {
					stmt, err := tx.Prepare(`
						INSERT OR REPLACE INTO entries (path, parent_path, name, size, is_dir) 
						VALUES (?, ?, ?, ?, ?)
					`)
					if err == nil {
						for _, e := range batch {
							isDir := 0
							if e.IsDir {
								isDir = 1
							}
							_, _ = stmt.Exec(e.Path, e.ParentPath, e.Name, e.Size, isDir)
						}
						_ = stmt.Close()
					}
					_ = tx.Commit()
				}
			}
			a.dbMutex.Unlock()
			batch = batch[:0]
		}

		for {
			select {
			case entry, ok := <-dbChan:
				if !ok {
					flush()
					return
				}
				batch = append(batch, entry)
				if len(batch) >= 2000 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	// Recursive Directory Scanner
	var scanDir func(path string) int64
	scanDir = func(path string) int64 {
		err := sem.Acquire(context.Background(), 1)
		if err != nil {
			return 0
		}
		dirInfo, err := os.ReadDir(path)
		sem.Release(1)

		if err != nil {
			return 0
		}

		var subDirChans []<-chan int64
		var directFilesSize int64

		for _, entry := range dirInfo {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			} else if entry.IsDir() {
				ch := make(chan int64, 1)
				subDirChans = append(subDirChans, ch)
				wg.Add(1)
				go func(subPath string, c chan<- int64) {
					defer wg.Done()
					c <- scanDir(subPath)
				}(filepath.Join(path, entry.Name()), ch)
			} else {
				info, err := entry.Info()
				if err == nil {
					size := info.Size()
					directFilesSize += size

					// Stream to SQLite batch channel
					filePath := filepath.Join(path, entry.Name())
					dbChan <- DBEntry{
						Path:       filePath,
						ParentPath: path,
						Name:       entry.Name(),
						Size:       size,
						IsDir:      false,
					}

					// Sample ~0.5% of files for rain animation
					if rand.Float32() < 0.005 {
						fileChan <- FileStat{Name: entry.Name(), Size: size}
					}
				}
			}
		}

		var subDirsTotalSize int64
		for _, ch := range subDirChans {
			subDirsTotalSize += <-ch
		}

		totalDirSize := directFilesSize + subDirsTotalSize

		// Record total recursive size for directory in SQLite
		parentPath := filepath.Dir(path)
		dbChan <- DBEntry{
			Path:       path,
			ParentPath: parentPath,
			Name:       filepath.Base(path),
			Size:       totalDirSize,
			IsDir:      true,
		}

		return totalDirSize
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanDir(root)
	}()

	wg.Wait()
	close(fileChan)
	close(dbChan)
	dbWg.Wait()

	runtime.EventsEmit(a.ctx, "scan-complete")
}
