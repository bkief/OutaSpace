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

type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
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
					runtime.EventsEmit(a.ctx, "scan-complete")
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

	var scanDir func(path string)
	scanDir = func(path string) {
		err := sem.Acquire(context.Background(), 1)
		if err != nil {
			return
		}

		dirInfo, err := os.ReadDir(path)
		sem.Release(1)

		if err != nil {
			return
		}

		for _, entry := range dirInfo {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			} else if entry.IsDir() {
				wg.Add(1)
				go func(subPath string) {
					defer wg.Done()
					scanDir(subPath)
				}(filepath.Join(path, entry.Name()))
			} else {
				info, err := entry.Info()
				if err == nil {
					// Sample about 0.5% of files to prevent screen crowding on large drives
					if rand.Float32() < 0.005 {
						fileChan <- FileStat{Name: entry.Name(), Size: info.Size()}
					}
				}
			}
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanDir(root)
	}()

	wg.Wait()
	close(fileChan)
}
