package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sync/semaphore"
)

type FileInfo struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	ModifiedDate int64  `json:"modtime"`
}

type DirTree struct {
	Name    string     `json:"name"`
	Path    string     `json:"path"`
	Size    int64      `json:"size"`
	SubDirs []DirTree  `json:"subdirs"`
	Files   []FileInfo `json:"files"`
}

var maxWorkers = runtime.NumCPU() * 2
var sem = semaphore.NewWeighted(int64(maxWorkers))
var ctx = context.TODO()

func main() {
	// 1. Allow custom path via args
	targetPath := "."
	if len(os.Args) > 1 {
		targetPath = os.Args[1]
	}

	root, err := filepath.Abs(targetPath)
	if err != nil {
		log.Fatalf("Invalid path: %v", err)
	}

	result := make(chan DirTree, 1)
	if _, err := os.Stat(root); err == nil {
		go osReadDir(root, result)
	} else {
		log.Fatalf("Path does not exist: %s", root)
	}

	finalTree := <-result

	f, err := os.Create("space_report.json")
	if err != nil {
		log.Fatalf("Error creating file: %v", err)
	}
	defer f.Close()

	// 2. Stream JSON directly to file to save memory
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(finalTree); err != nil {
		log.Fatalf("Error encoding JSON: %v", err)
	}
}

func osReadDir(root string, dirStructure chan<- DirTree) {
	defer close(dirStructure)
	tree := DirTree{Name: filepath.Base(root), Path: root}

	err := sem.Acquire(ctx, 1)
	if err != nil {
		log.Printf("Failed to acquire semaphore: %v", err)
		return
	}

	// 3. Use modern os.ReadDir
	dirInfo, err := os.ReadDir(root)
	
	// 4. FIX BUG: Immediately release semaphore after IO, regardless of success/fail
	sem.Release(1) 

	if err != nil {
		log.Printf("Skipping unreadable dir %s: %v", root, err)
		return
	}

	subDirChannels := make([]<-chan DirTree, 0)
	for _, entry := range dirInfo {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		} else if entry.IsDir() {
			dirs := make(chan DirTree, 1)
			go osReadDir(filepath.Join(root, entry.Name()), dirs)
			subDirChannels = append(subDirChannels, dirs)
		} else {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			f := FileInfo{Name: entry.Name(), Size: info.Size(), ModifiedDate: info.ModTime().Unix()}
			tree.Files = append(tree.Files, f)
			tree.Size += info.Size()
		}
	}

	for _, dirchan := range subDirChannels {
		for elem := range dirchan {
			tree.SubDirs = append(tree.SubDirs, elem)
			tree.Size += elem.Size
		}
	}

	dirStructure <- tree
}
