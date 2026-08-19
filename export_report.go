package main

import (
	"encoding/csv"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type ExtStat struct {
	Ext        string
	Count      int
	TotalBytes int64
	Percentage float64
}

type DirFileStat struct {
	Path       string
	Size       int64
	Percentage float64
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ExportHTMLReport queries the scan data, generates a standalone HTML summary report, and prompts user to save
func (a *App) ExportHTMLReport() (string, error) {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()

	if a.db == nil || a.rootPath == "" {
		return "", fmt.Errorf("no scan data available to export")
	}

	// 1. Get Root Size
	var totalSize int64
	_ = a.db.QueryRow("SELECT size FROM entries WHERE path = ?", a.rootPath).Scan(&totalSize)
	if totalSize <= 0 {
		totalSize = 1 // avoid div by zero
	}

	// 2. Counts
	var fileCount int
	var dirCount int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM entries WHERE is_dir = 0").Scan(&fileCount)
	_ = a.db.QueryRow("SELECT COUNT(*) FROM entries WHERE is_dir = 1").Scan(&dirCount)

	// 3. Extension Breakdown
	extMap := make(map[string]*ExtStat)
	rows, err := a.db.Query("SELECT name, size FROM entries WHERE is_dir = 0")
	if err == nil {
		for rows.Next() {
			var name string
			var size int64
			if err := rows.Scan(&name, &size); err == nil {
				ext := strings.ToLower(filepath.Ext(name))
				if ext == "" {
					ext = "[no extension]"
				}
				stat, exists := extMap[ext]
				if !exists {
					stat = &ExtStat{Ext: ext}
					extMap[ext] = stat
				}
				stat.Count++
				stat.TotalBytes += size
			}
		}
		rows.Close()
	}

	var extList []*ExtStat
	for _, stat := range extMap {
		stat.Percentage = (float64(stat.TotalBytes) / float64(totalSize)) * 100.0
		extList = append(extList, stat)
	}
	sort.Slice(extList, func(i, j int) bool {
		return extList[i].TotalBytes > extList[j].TotalBytes
	})

	// Consolidate beyond top 8 into "Other"
	var displayExtList []*ExtStat
	var otherExt ExtStat
	otherExt.Ext = "Other"
	for idx, item := range extList {
		if idx < 8 {
			displayExtList = append(displayExtList, item)
		} else {
			otherExt.Count += item.Count
			otherExt.TotalBytes += item.TotalBytes
		}
	}
	if otherExt.Count > 0 {
		otherExt.Percentage = (float64(otherExt.TotalBytes) / float64(totalSize)) * 100.0
		displayExtList = append(displayExtList, &otherExt)
	}

	// 4. Top 10 Largest Directories
	var topDirs []DirFileStat
	dirRows, err := a.db.Query("SELECT path, size FROM entries WHERE is_dir = 1 AND path != ? ORDER BY size DESC LIMIT 10", a.rootPath)
	if err == nil {
		for dirRows.Next() {
			var p string
			var s int64
			if err := dirRows.Scan(&p, &s); err == nil {
				pct := (float64(s) / float64(totalSize)) * 100.0
				topDirs = append(topDirs, DirFileStat{Path: p, Size: s, Percentage: pct})
			}
		}
		dirRows.Close()
	}

	// 5. Top 10 Largest Files
	var topFiles []DirFileStat
	fileRows, err := a.db.Query("SELECT path, size FROM entries WHERE is_dir = 0 ORDER BY size DESC LIMIT 10")
	if err == nil {
		for fileRows.Next() {
			var p string
			var s int64
			if err := fileRows.Scan(&p, &s); err == nil {
				pct := (float64(s) / float64(totalSize)) * 100.0
				topFiles = append(topFiles, DirFileStat{Path: p, Size: s, Percentage: pct})
			}
		}
		fileRows.Close()
	}

	// 6. Format Scan Time
	scanDate := time.Now().Format("2006-01-02 15:04:05 MST")
	durationStr := "N/A"
	if a.scanDuration > 0 {
		if a.scanDuration < time.Second {
			durationStr = fmt.Sprintf("%dms", a.scanDuration.Milliseconds())
		} else {
			durationStr = fmt.Sprintf("%.2fs", a.scanDuration.Seconds())
		}
	}

	// 7. Generate Standalone HTML Content
	htmlContent := generateReportHTML(a.rootPath, scanDate, durationStr, totalSize, fileCount, dirCount, displayExtList, topDirs, topFiles)

	// 8. Prompt Save File Dialog
	defaultName := fmt.Sprintf("outaspace-report-%s.html", time.Now().Format("20060102-150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save OutaSpace HTML Summary Report",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{
				DisplayName: "HTML Report (*.html)",
				Pattern:     "*.html",
			},
		},
	})

	if err != nil || savePath == "" {
		return "", nil // User cancelled
	}

	if err := os.WriteFile(savePath, []byte(htmlContent), 0644); err != nil {
		return "", fmt.Errorf("failed to save report: %w", err)
	}

	return savePath, nil
}

func generateReportHTML(
	rootPath string,
	scanDate string,
	duration string,
	totalSize int64,
	fileCount int,
	dirCount int,
	extList []*ExtStat,
	topDirs []DirFileStat,
	topFiles []DirFileStat,
) string {
	var extRows strings.Builder
	for _, ext := range extList {
		width := fmt.Sprintf("%.1f%%", ext.Percentage)
		extRows.WriteString(fmt.Sprintf(`
		<tr>
			<td><code>%s</code></td>
			<td class="num">%s</td>
			<td class="num font-mono">%s</td>
			<td class="num font-mono">%.1f%%</td>
			<td>
				<div class="progress-bar">
					<div class="progress-fill" style="width: %s;"></div>
				</div>
			</td>
		</tr>`,
			html.EscapeString(ext.Ext),
			formatNumber(ext.Count),
			formatBytes(ext.TotalBytes),
			ext.Percentage,
			width,
		))
	}

	var dirRows strings.Builder
	for idx, d := range topDirs {
		dirRows.WriteString(fmt.Sprintf(`
		<tr>
			<td class="num rank">#%d</td>
			<td class="num font-mono">%s</td>
			<td class="num font-mono">%.1f%%</td>
			<td class="path font-mono" title="%s">%s</td>
		</tr>`,
			idx+1,
			formatBytes(d.Size),
			d.Percentage,
			html.EscapeString(d.Path),
			html.EscapeString(d.Path),
		))
	}

	var fileRows strings.Builder
	for idx, f := range topFiles {
		fileRows.WriteString(fmt.Sprintf(`
		<tr>
			<td class="num rank">#%d</td>
			<td class="num font-mono">%s</td>
			<td class="num font-mono">%.1f%%</td>
			<td class="path font-mono" title="%s">%s</td>
		</tr>`,
			idx+1,
			formatBytes(f.Size),
			f.Percentage,
			html.EscapeString(f.Path),
			html.EscapeString(f.Path),
		))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>OutaSpace Scan Report - %s</title>
	<style>
		:root {
			--bg-color: #0b0c10;
			--card-bg: #151722;
			--card-border: rgba(102, 252, 241, 0.18);
			--text-main: #e2e8f0;
			--text-muted: #94a3b8;
			--primary: #66fcf1;
			--primary-glow: rgba(102, 252, 241, 0.35);
			--accent-pink: #f43f5e;
			--accent-amber: #f59e0b;
			--accent-blue: #38bdf8;
		}
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body {
			background: var(--bg-color);
			color: var(--text-main);
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			line-height: 1.6;
			padding: 40px 24px;
		}
		.container {
			max-width: 1040px;
			margin: 0 auto;
		}
		header {
			background: linear-gradient(135deg, rgba(21, 23, 34, 0.95), rgba(11, 12, 16, 0.98));
			border: 1px solid var(--card-border);
			border-radius: 16px;
			padding: 28px 32px;
			margin-bottom: 28px;
			box-shadow: 0 10px 30px rgba(0,0,0,0.5), 0 0 25px rgba(102, 252, 241, 0.08);
		}
		.brand-row {
			display: flex;
			align-items: center;
			justify-content: space-between;
			border-bottom: 1px solid rgba(255,255,255,0.08);
			padding-bottom: 16px;
			margin-bottom: 20px;
		}
		.brand-title {
			font-size: 26px;
			font-weight: 800;
			color: var(--primary);
			letter-spacing: 1.5px;
			text-transform: uppercase;
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.badge {
			background: rgba(102, 252, 241, 0.12);
			border: 1px solid var(--primary);
			color: var(--primary);
			font-size: 12px;
			font-weight: 600;
			padding: 4px 10px;
			border-radius: 9999px;
			text-transform: uppercase;
		}
		.meta-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 16px;
		}
		.meta-card {
			background: rgba(255,255,255,0.03);
			border: 1px solid rgba(255,255,255,0.06);
			border-radius: 12px;
			padding: 14px 16px;
		}
		.meta-label {
			font-size: 11px;
			font-weight: 700;
			color: var(--text-muted);
			text-transform: uppercase;
			letter-spacing: 0.8px;
			margin-bottom: 4px;
		}
		.meta-value {
			font-size: 16px;
			font-weight: 700;
			color: #fff;
			word-break: break-all;
		}
		.meta-value.highlight {
			color: var(--primary);
			font-size: 20px;
		}
		h2 {
			font-size: 19px;
			font-weight: 700;
			margin: 32px 0 16px 0;
			color: #fff;
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.section-card {
			background: var(--card-bg);
			border: 1px solid var(--card-border);
			border-radius: 14px;
			overflow: hidden;
			box-shadow: 0 8px 24px rgba(0,0,0,0.4);
		}
		table {
			width: 100%;
			border-collapse: collapse;
			text-align: left;
			font-size: 13.5px;
		}
		th {
			background: rgba(255,255,255,0.04);
			color: var(--text-muted);
			font-size: 11.5px;
			font-weight: 700;
			text-transform: uppercase;
			letter-spacing: 0.6px;
			padding: 12px 18px;
			border-bottom: 1px solid rgba(255,255,255,0.08);
		}
		td {
			padding: 12px 18px;
			border-bottom: 1px solid rgba(255,255,255,0.04);
		}
		tr:last-child td { border-bottom: none; }
		tr:hover td { background: rgba(102, 252, 241, 0.03); }
		.num { text-align: right; }
		.rank { font-weight: 700; color: var(--accent-amber); text-align: center; width: 40px; }
		.path { max-width: 500px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
		.font-mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
		code {
			background: rgba(255,255,255,0.08);
			padding: 3px 7px;
			border-radius: 6px;
			font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
			font-size: 12.5px;
			color: var(--primary);
		}
		.progress-bar {
			width: 140px;
			height: 8px;
			background: rgba(255,255,255,0.08);
			border-radius: 999px;
			overflow: hidden;
		}
		.progress-fill {
			height: 100%;
			background: linear-gradient(90deg, var(--primary), var(--accent-blue));
			border-radius: 999px;
		}
		footer {
			margin-top: 40px;
			text-align: center;
			font-size: 12px;
			color: var(--text-muted);
		}
		@media (max-width: 768px) {
			body { padding: 16px; }
			.meta-grid { grid-template-columns: 1fr; }
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="brand-row">
				<div class="brand-title">
					🪐 OutaSpace Scan Report
				</div>
				<span class="badge">Disk Audit</span>
			</div>
			<div class="meta-grid">
				<div class="meta-card">
					<div class="meta-label">Target Path</div>
					<div class="meta-value font-mono" title="%s">%s</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Total Storage</div>
					<div class="meta-value highlight font-mono">%s</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Scanned Items</div>
					<div class="meta-value font-mono">%s files, %s dirs</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Scan Date & Duration</div>
					<div class="meta-value font-mono">%s (%s)</div>
				</div>
			</div>
		</header>

		<h2>📊 Extension Breakdown</h2>
		<div class="section-card">
			<table>
				<thead>
					<tr>
						<th>Extension</th>
						<th class="num">Files</th>
						<th class="num">Total Size</th>
						<th class="num">%% of Scanned</th>
						<th style="width: 160px;">Visual</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>

		<h2>🚨 Top 10 Largest Directories</h2>
		<div class="section-card">
			<table>
				<thead>
					<tr>
						<th class="rank">#</th>
						<th class="num">Size</th>
						<th class="num">%% of Total</th>
						<th>Directory Path</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>

		<h2>📄 Top 10 Largest Files</h2>
		<div class="section-card">
			<table>
				<thead>
					<tr>
						<th class="rank">#</th>
						<th class="num">Size</th>
						<th class="num">%% of Total</th>
						<th>File Path</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>

		<footer>
			Generated by <strong style="color: var(--primary);">OutaSpace</strong> on %s
		</footer>
	</div>
</body>
</html>`,
		html.EscapeString(filepath.Base(rootPath)),
		html.EscapeString(rootPath),
		html.EscapeString(rootPath),
		formatBytes(totalSize),
		formatNumber(fileCount),
		formatNumber(dirCount),
		scanDate,
		duration,
		extRows.String(),
		dirRows.String(),
		fileRows.String(),
		scanDate,
	)
}

func formatNumber(n int) string {
	in := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range in {
		if i > 0 && (len(in)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

type TreeNode struct {
	Name        string
	Path        string
	Size        int64
	IsDir       bool
	ModTime     int64
	DirectFiles int
	DirectDirs  int
	TotalFiles  int
	TotalDirs   int
	Children    []*TreeNode
}

func (a *App) buildInMemoryTree() (*TreeNode, int64, int, int, error) {
	var rootSize int64
	_ = a.db.QueryRow("SELECT size FROM entries WHERE path = ?", a.rootPath).Scan(&rootSize)
	if rootSize <= 0 {
		rootSize = 1
	}

	var totalFileCount int
	var totalDirCount int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM entries WHERE is_dir = 0").Scan(&totalFileCount)
	_ = a.db.QueryRow("SELECT COUNT(*) FROM entries WHERE is_dir = 1").Scan(&totalDirCount)

	nodesByPath := make(map[string]*TreeNode)
	childrenByParent := make(map[string][]*TreeNode)

	rows, err := a.db.Query("SELECT path, parent_path, name, size, is_dir, mod_time FROM entries")
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p, parent, name string
		var sz int64
		var isDirInt int
		var modTime int64
		if err := rows.Scan(&p, &parent, &name, &sz, &isDirInt, &modTime); err == nil {
			node := &TreeNode{
				Name:    name,
				Path:    p,
				Size:    sz,
				IsDir:   (isDirInt == 1),
				ModTime: modTime,
			}
			nodesByPath[p] = node
			childrenByParent[parent] = append(childrenByParent[parent], node)
		}
	}

	for parent, children := range childrenByParent {
		sort.Slice(children, func(i, j int) bool {
			if children[i].IsDir != children[j].IsDir {
				return children[i].IsDir
			}
			return children[i].Size > children[j].Size
		})
		childrenByParent[parent] = children
	}

	for p, node := range nodesByPath {
		if ch, exists := childrenByParent[p]; exists {
			node.Children = ch
			for _, c := range ch {
				if c.IsDir {
					node.DirectDirs++
				} else {
					node.DirectFiles++
				}
			}
		}
	}

	rootNode, exists := nodesByPath[a.rootPath]
	if !exists {
		rootNode = &TreeNode{
			Name:     filepath.Base(a.rootPath),
			Path:     a.rootPath,
			Size:     rootSize,
			IsDir:    true,
			Children: childrenByParent[a.rootPath],
		}
	}

	// Compute recursive counts for each node
	var computeRecursiveCounts func(n *TreeNode) (int, int)
	computeRecursiveCounts = func(n *TreeNode) (int, int) {
		if !n.IsDir {
			n.TotalFiles = 1
			n.TotalDirs = 0
			return 1, 0
		}
		tf := 0
		td := 0
		for _, child := range n.Children {
			f, d := computeRecursiveCounts(child)
			tf += f
			if child.IsDir {
				td += (d + 1)
			}
		}
		n.TotalFiles = tf
		n.TotalDirs = td
		return tf, td
	}
	computeRecursiveCounts(rootNode)

	return rootNode, rootSize, totalFileCount, totalDirCount, nil
}

// ExportHTMLTreeReport builds a hierarchical ASCII tree report with visual proportional bars and prompts user to save
func (a *App) ExportHTMLTreeReport() (string, error) {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()

	if a.db == nil || a.rootPath == "" {
		return "", fmt.Errorf("no scan data available to export")
	}

	rootNode, rootSize, fileCount, dirCount, err := a.buildInMemoryTree()
	if err != nil {
		return "", err
	}

	// Render ASCII Tree (HTML and Plain text)
	var htmlTree strings.Builder
	var plainTree strings.Builder
	renderAsciiTree(rootNode, "", true, true, rootSize, &htmlTree, &plainTree)

	scanDate := time.Now().Format("2006-01-02 15:04:05 MST")
	durationStr := "N/A"
	if a.scanDuration > 0 {
		if a.scanDuration < time.Second {
			durationStr = fmt.Sprintf("%dms", a.scanDuration.Milliseconds())
		} else {
			durationStr = fmt.Sprintf("%.2fs", a.scanDuration.Seconds())
		}
	}

	fullHTML := generateTreeHTMLDocument(a.rootPath, scanDate, durationStr, rootSize, fileCount, dirCount, htmlTree.String(), plainTree.String())

	defaultName := fmt.Sprintf("outaspace-tree-%s.html", time.Now().Format("20060102-150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save OutaSpace HTML Tree Report",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{
				DisplayName: "HTML Tree Report (*.html)",
				Pattern:     "*.html",
			},
		},
	})

	if err != nil || savePath == "" {
		return "", nil // User cancelled
	}

	if err := os.WriteFile(savePath, []byte(fullHTML), 0644); err != nil {
		return "", fmt.Errorf("failed to save report: %w", err)
	}

	return savePath, nil
}

// ExportTextTreeReport builds a plain-text ASCII tree report (.txt) and prompts user to save
func (a *App) ExportTextTreeReport() (string, error) {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()

	if a.db == nil || a.rootPath == "" {
		return "", fmt.Errorf("no scan data available to export")
	}

	rootNode, rootSize, _, _, err := a.buildInMemoryTree()
	if err != nil {
		return "", err
	}

	var htmlTree strings.Builder
	var plainTree strings.Builder
	renderAsciiTree(rootNode, "", true, true, rootSize, &htmlTree, &plainTree)

	defaultName := fmt.Sprintf("outaspace-tree-%s.txt", time.Now().Format("20060102-150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save OutaSpace Text Tree Report",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Text Files (*.txt)",
				Pattern:     "*.txt",
			},
		},
	})

	if err != nil || savePath == "" {
		return "", nil
	}

	if err := os.WriteFile(savePath, []byte(plainTree.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to save text report: %w", err)
	}

	return savePath, nil
}

// ExportRawDataCSV exports all scanned file and directory stats into a structured CSV file
func (a *App) ExportRawDataCSV() (string, error) {
	a.dbMutex.RLock()
	defer a.dbMutex.RUnlock()

	if a.db == nil || a.rootPath == "" {
		return "", fmt.Errorf("no scan data available to export")
	}

	rootNode, rootSize, _, _, err := a.buildInMemoryTree()
	if err != nil {
		return "", err
	}

	defaultName := fmt.Sprintf("outaspace-raw-data-%s.csv", time.Now().Format("20060102-150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save OutaSpace Raw Data CSV",
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{
				DisplayName: "CSV Files (*.csv)",
				Pattern:     "*.csv",
			},
		},
	})

	if err != nil || savePath == "" {
		return "", nil // User cancelled
	}

	file, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header row
	header := []string{
		"path",
		"type",
		"size_bytes",
		"size_human",
		"files_count",
		"dirs_count",
		"pct_total",
		"last_modified",
	}
	if err := writer.Write(header); err != nil {
		return "", err
	}

	// Write rows recursively in depth-first order
	var writeNode func(n *TreeNode) error
	writeNode = func(n *TreeNode) error {
		typeStr := "file"
		filesCount := 1
		dirsCount := 0
		if n.IsDir {
			typeStr = "dir"
			filesCount = n.TotalFiles
			dirsCount = n.TotalDirs
		}

		pct := (float64(n.Size) / float64(rootSize)) * 100.0
		if pct > 100.0 {
			pct = 100.0
		}

		modTimeStr := ""
		if n.ModTime > 0 {
			modTimeStr = time.Unix(n.ModTime, 0).UTC().Format(time.RFC3339)
		}

		row := []string{
			n.Path,
			typeStr,
			strconv.FormatInt(n.Size, 10),
			formatBytes(n.Size),
			strconv.Itoa(filesCount),
			strconv.Itoa(dirsCount),
			fmt.Sprintf("%.2f", pct),
			modTimeStr,
		}

		if err := writer.Write(row); err != nil {
			return err
		}

		for _, child := range n.Children {
			if err := writeNode(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := writeNode(rootNode); err != nil {
		return "", fmt.Errorf("failed writing csv rows: %w", err)
	}

	return savePath, nil
}

func makeAsciiBar(pct float64) string {
	filled := int((pct / 100.0) * 10.0 + 0.5)
	if filled > 10 {
		filled = 10
	}
	if filled < 0 {
		filled = 0
	}
	return fmt.Sprintf("[%s%s]", strings.Repeat("█", filled), strings.Repeat("░", 10-filled))
}

func renderAsciiTree(node *TreeNode, prefix string, isLast bool, isRoot bool, rootSize int64, htmlBuilder *strings.Builder, plainBuilder *strings.Builder) {
	sizeStr := fmt.Sprintf("%10s", formatBytes(node.Size))
	pct := 0.0
	if rootSize > 0 {
		pct = (float64(node.Size) / float64(rootSize)) * 100.0
	}
	bar := makeAsciiBar(pct)

	displayName := node.Name
	if node.IsDir && !strings.HasSuffix(displayName, "/") && !strings.HasSuffix(displayName, "\\") {
		displayName += "/"
	}

	barClass := "bar-normal"
	if pct >= 25.0 {
		barClass = "bar-high"
	} else if pct >= 10.0 {
		barClass = "bar-mid"
	}

	hasChildren := len(node.Children) > 0

	if isRoot {
		plainLine := fmt.Sprintf("%s  %s\n", sizeStr, displayName)
		plainBuilder.WriteString(plainLine)

		if hasChildren {
			htmlBuilder.WriteString(`<details class="tree-dir is-root-dir" open>` + "\n")
			htmlBuilder.WriteString(fmt.Sprintf(`  <summary class="tree-node is-root"><span class="tree-size highlight font-mono">%s</span> <span class="folder-arrow">▼</span> <span class="tree-name is-dir font-mono">%s</span></summary>`+"\n",
				html.EscapeString(sizeStr), html.EscapeString(displayName)))
			htmlBuilder.WriteString(`  <div class="tree-children">` + "\n")
		} else {
			htmlBuilder.WriteString(fmt.Sprintf(`<div class="tree-node is-root"><span class="tree-size highlight font-mono">%s</span>  <span class="tree-name is-dir font-mono">%s</span></div>`+"\n",
				html.EscapeString(sizeStr), html.EscapeString(displayName)))
		}
	} else {
		branch := "├── "
		if isLast {
			branch = "└── "
		}

		plainLine := fmt.Sprintf("%s%s %s %s  %s\n", prefix, branch, sizeStr, bar, displayName)
		plainBuilder.WriteString(plainLine)

		if node.IsDir && hasChildren {
			htmlBuilder.WriteString(`<details class="tree-dir" open>` + "\n")
			htmlBuilder.WriteString(fmt.Sprintf(`  <summary class="tree-node is-dir"><span class="tree-indent font-mono">%s%s</span> <span class="tree-size font-mono">%s</span> <span class="tree-bar %s font-mono">%s</span> <span class="tree-pct font-mono">%.1f%%</span> <span class="folder-arrow">▼</span> <span class="tree-name is-dir font-mono" title="%s">%s</span></summary>`+"\n",
				html.EscapeString(prefix), html.EscapeString(branch),
				html.EscapeString(sizeStr),
				barClass, html.EscapeString(bar),
				pct,
				html.EscapeString(node.Path), html.EscapeString(displayName)))
			htmlBuilder.WriteString(`  <div class="tree-children">` + "\n")
		} else {
			typeClass := "is-file"
			if node.IsDir {
				typeClass = "is-dir"
			}
			htmlBuilder.WriteString(fmt.Sprintf(`  <div class="tree-node %s"><span class="tree-indent font-mono">%s%s</span> <span class="tree-size font-mono">%s</span> <span class="tree-bar %s font-mono">%s</span> <span class="tree-pct font-mono">%.1f%%</span> <span class="folder-spacer">  </span> <span class="tree-name %s font-mono" title="%s">%s</span></div>`+"\n",
				typeClass,
				html.EscapeString(prefix), html.EscapeString(branch),
				html.EscapeString(sizeStr),
				barClass, html.EscapeString(bar),
				pct,
				typeClass, html.EscapeString(node.Path), html.EscapeString(displayName)))
		}
	}

	childPrefix := prefix
	if !isRoot {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range node.Children {
		renderAsciiTree(child, childPrefix, i == len(node.Children)-1, false, rootSize, htmlBuilder, plainBuilder)
	}

	if node.IsDir && hasChildren {
		htmlBuilder.WriteString("  </div>\n</details>\n")
	}
}

func generateTreeHTMLDocument(
	rootPath string,
	scanDate string,
	duration string,
	totalSize int64,
	fileCount int,
	dirCount int,
	htmlTreeContent string,
	rawAsciiContent string,
) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>OutaSpace Tree Report - %s</title>
	<style>
		:root {
			--bg-color: #0b0c10;
			--card-bg: #151722;
			--card-border: rgba(102, 252, 241, 0.18);
			--text-main: #e2e8f0;
			--text-muted: #94a3b8;
			--primary: #66fcf1;
			--accent-pink: #f43f5e;
			--accent-amber: #f59e0b;
			--accent-blue: #38bdf8;
		}
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body {
			background: var(--bg-color);
			color: var(--text-main);
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			line-height: 1.6;
			padding: 32px 24px;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
		}
		header {
			background: linear-gradient(135deg, rgba(21, 23, 34, 0.95), rgba(11, 12, 16, 0.98));
			border: 1px solid var(--card-border);
			border-radius: 16px;
			padding: 24px 28px;
			margin-bottom: 24px;
			box-shadow: 0 10px 30px rgba(0,0,0,0.5), 0 0 25px rgba(102, 252, 241, 0.08);
		}
		.brand-row {
			display: flex;
			align-items: center;
			justify-content: space-between;
			border-bottom: 1px solid rgba(255,255,255,0.08);
			padding-bottom: 14px;
			margin-bottom: 16px;
		}
		.brand-title {
			font-size: 24px;
			font-weight: 800;
			color: var(--primary);
			letter-spacing: 1.5px;
			text-transform: uppercase;
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.badge {
			background: rgba(102, 252, 241, 0.12);
			border: 1px solid var(--primary);
			color: var(--primary);
			font-size: 12px;
			font-weight: 600;
			padding: 4px 10px;
			border-radius: 9999px;
			text-transform: uppercase;
		}
		.meta-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 14px;
		}
		.meta-card {
			background: rgba(255,255,255,0.03);
			border: 1px solid rgba(255,255,255,0.06);
			border-radius: 12px;
			padding: 12px 16px;
		}
		.meta-label {
			font-size: 11px;
			font-weight: 700;
			color: var(--text-muted);
			text-transform: uppercase;
			letter-spacing: 0.8px;
			margin-bottom: 4px;
		}
		.meta-value {
			font-size: 15px;
			font-weight: 700;
			color: #fff;
			word-break: break-all;
		}
		.meta-value.highlight {
			color: var(--primary);
			font-size: 18px;
		}
		.toolbar {
			display: flex;
			align-items: center;
			justify-content: space-between;
			gap: 12px;
			margin-bottom: 14px;
			flex-wrap: wrap;
		}
		.search-box {
			flex: 1;
			min-width: 260px;
			background: rgba(21, 23, 34, 0.9);
			border: 1px solid var(--card-border);
			border-radius: 10px;
			padding: 8px 14px;
			color: #fff;
			font-size: 13px;
			outline: none;
			transition: all 0.2s;
		}
		.search-box:focus {
			border-color: var(--primary);
			box-shadow: 0 0 12px rgba(102, 252, 241, 0.3);
		}
		.btn-group {
			display: flex;
			align-items: center;
			gap: 8px;
		}
		.btn {
			background: rgba(21, 23, 34, 0.9);
			border: 1px solid var(--card-border);
			color: var(--primary);
			padding: 8px 14px;
			border-radius: 10px;
			font-size: 12px;
			font-weight: 700;
			cursor: pointer;
			display: inline-flex;
			align-items: center;
			gap: 6px;
			transition: all 0.2s;
			user-select: none;
		}
		.btn:hover {
			background: var(--primary);
			color: #0b0c10;
			box-shadow: 0 0 14px rgba(102, 252, 241, 0.4);
		}
		.tree-card {
			background: #08090d;
			border: 1px solid var(--card-border);
			border-radius: 14px;
			padding: 20px;
			overflow-x: auto;
			box-shadow: 0 8px 24px rgba(0,0,0,0.5);
			font-size: 13px;
			line-height: 1.7;
		}
		.font-mono {
			font-family: ui-monospace, SFMono-Regular, "Cascadia Code", Menlo, Monaco, Consolas, monospace;
		}
		details.tree-dir {
			margin: 0;
			padding: 0;
		}
		summary.tree-node {
			cursor: pointer;
			list-style: none;
		}
		summary.tree-node::-webkit-details-marker {
			display: none;
		}
		.tree-node {
			white-space: pre;
			display: flex;
			align-items: center;
			padding: 2px 4px;
			border-radius: 4px;
			transition: background 0.1s;
		}
		.tree-node:hover {
			background: rgba(102, 252, 241, 0.06);
		}
		.tree-node.is-root {
			font-size: 15px;
			font-weight: 700;
			margin-bottom: 6px;
			padding-bottom: 6px;
			border-bottom: 1px solid rgba(255,255,255,0.08);
		}
		.folder-arrow {
			display: inline-block;
			color: var(--primary);
			font-size: 10px;
			width: 14px;
			text-align: center;
			margin-right: 4px;
			transition: transform 0.2s ease;
			user-select: none;
		}
		details:not([open]) > summary .folder-arrow {
			transform: rotate(-90deg);
		}
		.folder-spacer {
			display: inline-block;
			width: 18px;
		}
		.tree-indent {
			color: rgba(255,255,255,0.25);
			user-select: none;
		}
		.tree-size {
			color: var(--text-main);
			font-weight: 600;
			display: inline-block;
			width: 85px;
			text-align: right;
			margin-right: 6px;
		}
		.tree-size.highlight {
			color: var(--primary);
			font-size: 16px;
			width: auto;
		}
		.tree-bar {
			display: inline-block;
			letter-spacing: 0.5px;
			margin-right: 8px;
			user-select: none;
		}
		.bar-normal { color: var(--primary); }
		.bar-mid { color: var(--accent-amber); }
		.bar-high { color: var(--accent-pink); }
		.tree-pct {
			color: var(--text-muted);
			display: inline-block;
			width: 50px;
			text-align: right;
			margin-right: 8px;
			font-size: 11.5px;
		}
		.tree-name {
			color: #fff;
		}
		.tree-name.is-dir {
			color: var(--primary);
			font-weight: 700;
		}
		.tree-name.is-file {
			color: var(--text-main);
		}
		footer {
			margin-top: 32px;
			text-align: center;
			font-size: 12px;
			color: var(--text-muted);
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="brand-row">
				<div class="brand-title">
					🪐 OutaSpace Collapsible Tree Report
				</div>
				<span class="badge">Interactive ASCII Tree</span>
			</div>
			<div class="meta-grid">
				<div class="meta-card">
					<div class="meta-label">Target Path</div>
					<div class="meta-value font-mono" title="%s">%s</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Total Storage</div>
					<div class="meta-value highlight font-mono">%s</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Scanned Items</div>
					<div class="meta-value font-mono">%s files, %s dirs</div>
				</div>
				<div class="meta-card">
					<div class="meta-label">Scan Date & Duration</div>
					<div class="meta-value font-mono">%s (%s)</div>
				</div>
			</div>
		</header>

		<div class="toolbar">
			<input type="text" id="filter-input" class="search-box font-mono" placeholder="🔍 Live filter files/directories...">
			<div class="btn-group">
				<button type="button" id="expand-all-btn" class="btn">➕ Expand All</button>
				<button type="button" id="collapse-all-btn" class="btn">➖ Collapse All</button>
				<button type="button" id="copy-ascii-btn" class="btn">📋 Copy ASCII Tree</button>
			</div>
		</div>

		<div class="tree-card font-mono" id="tree-container">
%s
		</div>

		<textarea id="raw-ascii-store" style="display:none;">%s</textarea>

		<footer>
			Generated by <strong style="color: var(--primary);">OutaSpace</strong> on %s
		</footer>
	</div>

	<script>
		// Expand / Collapse all
		const expandBtn = document.getElementById('expand-all-btn');
		const collapseBtn = document.getElementById('collapse-all-btn');
		const allDetails = document.querySelectorAll('details.tree-dir');

		expandBtn.addEventListener('click', () => {
			allDetails.forEach(d => d.open = true);
		});

		collapseBtn.addEventListener('click', () => {
			allDetails.forEach(d => {
				if (!d.classList.contains('is-root-dir')) {
					d.open = false;
				}
			});
		});

		// Live search / filter with auto-expansion of matching branches
		const filterInput = document.getElementById('filter-input');
		const allNodes = document.querySelectorAll('.tree-node:not(.is-root)');

		filterInput.addEventListener('input', (e) => {
			const val = e.target.value.toLowerCase().trim();
			if (!val) {
				allNodes.forEach(node => {
					node.closest('.tree-node, details.tree-dir').style.display = '';
					node.style.display = '';
				});
				return;
			}

			allNodes.forEach(node => {
				const text = node.textContent.toLowerCase();
				const isMatch = text.includes(val);
				node.style.display = isMatch ? 'flex' : 'none';

				if (isMatch) {
					// Open all ancestor details so matching item is visible
					let parent = node.parentElement;
					while (parent && parent !== document.body) {
						if (parent.tagName === 'DETAILS') {
							parent.open = true;
							parent.style.display = '';
						}
						parent = parent.parentElement;
					}
				}
			});
		});

		// Copy ASCII text
		const copyBtn = document.getElementById('copy-ascii-btn');
		const rawStore = document.getElementById('raw-ascii-store');
		copyBtn.addEventListener('click', () => {
			navigator.clipboard.writeText(rawStore.value).then(() => {
				const oldText = copyBtn.innerText;
				copyBtn.innerText = '✅ Copied to Clipboard!';
				setTimeout(() => copyBtn.innerText = oldText, 2500);
			}).catch(err => {
				alert('Failed to copy: ' + err);
			});
		});
	</script>
</body>
</html>`,
		html.EscapeString(filepath.Base(rootPath)),
		html.EscapeString(rootPath),
		html.EscapeString(rootPath),
		formatBytes(totalSize),
		formatNumber(fileCount),
		formatNumber(dirCount),
		scanDate,
		duration,
		htmlTreeContent,
		html.EscapeString(rawAsciiContent),
		scanDate,
	)
}
