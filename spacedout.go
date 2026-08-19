package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	//"sync"

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

	root, _ := filepath.Abs(".")

	result := make(chan DirTree, 1)
	if _, err := os.Stat(root); err == nil {
		go osReadDir(root, result)
	} else {
		log.Fatal("Path does not exist!")
	}

	finalTree := <-result
	treeJson, _ := json.MarshalIndent(finalTree, "", "    ")
	f, err := os.Create("space_report.json")
	if err != nil {
		log.Fatal("Err opening file")
	}
	defer f.Close()

	f.Write(treeJson)
	f.Sync()
}

func osReadDir(root string, dirStructure chan<- DirTree) {
	defer close(dirStructure)
	tree := DirTree{filepath.Base(root), root, 0, nil, nil}

	err := sem.Acquire(ctx, 1)
	if err != nil {
		log.Printf("Failed to acquire semaphore: %v", err)
		return
	}

	f, err := os.Open(root)
	if err != nil {
		return
	}
	dirInfo, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return
	}

	i := 0

	subDirChannels := make([]<-chan DirTree, 0)
	for _, fi := range dirInfo {
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		} else if fi.IsDir() {
			i++
			dirs := make(chan DirTree, 1)
			go osReadDir(filepath.Join(root, fi.Name()), dirs)
			subDirChannels = append(subDirChannels, dirs)
		} else {
			f := FileInfo{fi.Name(), fi.Size(), fi.ModTime().Unix()}
			tree.Files = append(tree.Files, f)
			tree.Size = tree.Size + fi.Size()
		}
	}

	sem.Release(1)
	//fmt.Println(root, " - ", i)

	//fmt.Println(root, " - ", len(dirs))
	for _, dirchan := range subDirChannels {
		for elem := range dirchan {
			tree.SubDirs = append(tree.SubDirs, elem)
			tree.Size = tree.Size + elem.Size
		}

	}

	fmt.Println(root, ", ", len(dirStructure))
	dirStructure <- tree

}
