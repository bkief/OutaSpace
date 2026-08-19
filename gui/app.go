package main

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/semaphore"
)

type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type DirTree struct {
	Name    string     `json:"name"`
	Path    string     `json:"path"`
	Size    int64      `json:"size"`
	SubDirs []*DirTree `json:"subdirs"`
	Files   []FileInfo `json:"files"`
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

type App struct {
	ctx          context.Context
	folderMap    map[string]*DirTree
	rootPath     string
	resultsMutex sync.RWMutex
}

func NewApp() *App {
	return &App{
		folderMap: make(map[string]*DirTree),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

type FileStat struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
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

// GetRootFolderView returns the shallow view of the root directory
func (a *App) GetRootFolderView() *FolderView {
	a.resultsMutex.RLock()
	defer a.resultsMutex.RUnlock()
	return a.buildFolderView(a.rootPath)
}

// GetFolderView returns the shallow view of a specific directory path
func (a *App) GetFolderView(path string) *FolderView {
	a.resultsMutex.RLock()
	defer a.resultsMutex.RUnlock()
	return a.buildFolderView(path)
}

func (a *App) buildFolderView(path string) *FolderView {
	tree, ok := a.folderMap[path]
	if !ok || tree == nil {
		return nil
	}

	view := &FolderView{
		Path:     tree.Path,
		Name:     tree.Name,
		Size:     tree.Size,
		Children: make([]ItemInfo, 0, len(tree.SubDirs)+len(tree.Files)),
	}

	if path != a.rootPath {
		view.Parent = filepath.Dir(path)
	}

	for _, sub := range tree.SubDirs {
		view.Children = append(view.Children, ItemInfo{
			Name:  sub.Name,
			Path:  sub.Path,
			Size:  sub.Size,
			IsDir: true,
		})
	}

	for _, f := range tree.Files {
		view.Children = append(view.Children, ItemInfo{
			Name:  f.Name,
			Path:  filepath.Join(tree.Path, f.Name),
			Size:  f.Size,
			IsDir: false,
		})
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

	a.resultsMutex.Lock()
	a.folderMap = make(map[string]*DirTree)
	a.rootPath = root
	a.resultsMutex.Unlock()

	fileChan := make(chan FileStat, 5000)
	var wg sync.WaitGroup

	// Background worker to consume fileChan and emit to frontend in batches
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

	var scanDir func(path string) *DirTree
	scanDir = func(path string) *DirTree {
		tree := &DirTree{
			Name:    filepath.Base(path),
			Path:    path,
			SubDirs: make([]*DirTree, 0),
			Files:   make([]FileInfo, 0),
		}

		err := sem.Acquire(context.Background(), 1)
		if err != nil {
			return tree
		}
		dirInfo, err := os.ReadDir(path)
		sem.Release(1)

		if err != nil {
			return tree
		}

		var subDirChans []<-chan *DirTree

		for _, entry := range dirInfo {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			} else if entry.IsDir() {
				ch := make(chan *DirTree, 1)
				subDirChans = append(subDirChans, ch)
				wg.Add(1)
				go func(subPath string, c chan<- *DirTree) {
					defer wg.Done()
					c <- scanDir(subPath)
				}(filepath.Join(path, entry.Name()), ch)
			} else {
				info, err := entry.Info()
				if err == nil {
					size := info.Size()
					tree.Files = append(tree.Files, FileInfo{Name: entry.Name(), Size: size})
					tree.Size += size

					// Sample about 0.5% of files for the raining visual
					if rand.Float32() < 0.005 {
						fileChan <- FileStat{Name: entry.Name(), Size: size}
					}
				}
			}
		}

		for _, ch := range subDirChans {
			subTree := <-ch
			if subTree != nil {
				tree.SubDirs = append(tree.SubDirs, subTree)
				tree.Size += subTree.Size
			}
		}

		a.resultsMutex.Lock()
		a.folderMap[tree.Path] = tree
		a.resultsMutex.Unlock()

		return tree
	}

	// Wait for root directory to finish
	rootCh := make(chan *DirTree, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		rootCh <- scanDir(root)
	}()

	wg.Wait()
	close(fileChan)

	runtime.EventsEmit(a.ctx, "scan-complete")
}
