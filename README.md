# OutaSpace 🪐

<p align="center">
  <img src="./build/appicon.png" width="128" height="128" alt="OutaSpace Icon" style="border-radius: 28px;" />
</p>

<p align="center">
  <strong>High-Performance Disk Space Analyzer with Real-Time Rain & Interactive Treemaps</strong>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#architecture">Architecture</a> •
  <a href="#installation--building">Build</a> •
  <a href="#license">License</a>
</p>

---

## 🌟 Overview

**OutaSpace** is a modern, cross-platform disk space visualization and storage auditing utility. Designed from the ground up for massive filesystems, OutaSpace combines live visual feedback during scans with an interactive hierarchical cushion treemap to pinpoint large files, hidden storage hogs, and bloated directory trees in seconds.

---

## ✨ Features

### 🌧️ Live File Stat Rain
- **Real-Time Visual Feedback**: Files fall into the cosmic canvas in real-time as your disk is scanned.
- **Dynamic Saturation Rate-Limiting**: Intelligently scales emission—admitting 100% of files on small folders or start-up, while smoothly gating particle density when the screen is saturated to maintain 60 FPS performance.
- **Adjustable Concurrency**: Configurable scan speeds (`Slow`, `Medium`, `Fast`) mapped dynamically to worker thread limits and CPU cores.

### 📊 Interactive Cushion Treemap & Results Dashboard
- **Hierarchical Cushion Treemap**: Powered by Apache ECharts with rich bevel borders and intuitive size-proportional tile mapping.
- **Drill-Down Navigation**: Single-click on any folder tile to dive into its sub-tree, complete with instant breadcrumbs and an "⬆ Up" level navigator.
- **Directory Contents Table**: Monospace file size formatting, proportion progress bars, and file type classification.
- **Extension Breakdown Legend**: Color-coded breakdown of storage distribution across file extensions (archives, media, code, binaries, documents).

### ⚡ Pure-Go SQLite In-Memory / Local Indexing
- **Zero Memory Bloat**: Entire directory trees are indexed into a local, pure-Go SQLite database (`modernc.org/sqlite` — no CGo required).
- **High-Throughput Batch Ingestion**: Ingests tens of thousands of entries per second using background worker transactions.
- **Bottom-Up Size Rollups**: Fast recursive directory calculations allow instant `< 1ms` single-folder queries regardless of filesystem scale.
- **Clean Lifecycle**: Temporary database tables are automatically initialized on launch and cleaned up on shutdown.

---

## 🛠️ Tech Stack

| Layer | Technology |
| :--- | :--- |
| **Framework** | [Wails v2](https://wails.io/) (Go + Native WebViews) |
| **Backend Engine** | Go 1.24+ with `modernc.org/sqlite` (Pure Go, No CGo) |
| **Frontend** | HTML5, Vanilla JavaScript, CSS3 |
| **Visualizations** | HTML5 Canvas (Rain Engine) + [Apache ECharts](https://echarts.apache.org/) (Treemaps) |

---

## 🚀 Installation & Building

### Prerequisites
1. **[Go](https://go.dev/dl/)** (v1.21 or later)
2. **[Wails CLI v2](https://wails.io/docs/gettingstarted/installation)**:
   ```bash
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```

### 💻 Local Development
Run live development mode with hot reloading:
```bash
wails dev
```

### 📦 Production Build

Build the optimized, native production binary for your operating system:

```bash
# Windows (.exe)
wails build

# macOS (Apple Silicon or Intel .app)
wails build -platform darwin/universal

# Linux (Binary)
wails build -platform linux/amd64
```

The compiled binary will be placed in `build/bin/OutaSpace.exe` (or `build/bin/OutaSpace`).

---

## 📜 License

This project is licensed under the **GNU General Public License v3.0** (GPLv3).  
See the [LICENSE](LICENSE) file for complete terms and conditions.
