package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
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
