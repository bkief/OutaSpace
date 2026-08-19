let canvas, ctx;
let circles = [];
let width, height;
let echartsInstance = null;
let currentFolderView = null;

// Palette for falling rain circles
const rainColors = [
    '#66fcf1', '#45a29e', '#f43f5e', '#10b981', '#f59e0b', '#a855f7', '#38bdf8'
];

// Color mapping for file extensions
const extColorPalette = {
    // Media & Video
    mp4: '#38bdf8', mkv: '#0284c7', avi: '#0369a1', mov: '#0ea5e9', webm: '#38bdf8',
    // Audio
    mp3: '#d946ef', wav: '#c026d3', flac: '#a21caf', aac: '#e879f9', ogg: '#f472b6',
    // Archives & Compressed
    zip: '#ef4444', rar: '#dc2626', '7z': '#b91c1c', tar: '#991b1b', gz: '#f87171', iso: '#e11d48',
    // Documents & PDFs
    pdf: '#f59e0b', doc: '#d97706', docx: '#b45309', txt: '#fbbf24', md: '#fde047', csv: '#eab308',
    // Images & Graphics
    png: '#10b981', jpg: '#059669', jpeg: '#047857', gif: '#34d399', svg: '#6ee7b7', webp: '#10b981', psd: '#059669',
    // Code & Dev
    go: '#00add8', js: '#facc15', ts: '#3b82f6', py: '#60a5fa', html: '#f97316', css: '#ec4899', json: '#a855f7',
    // Executables & Binaries
    exe: '#8b5cf6', dll: '#7c3aed', sys: '#6d28d9', bin: '#5b21b6', msi: '#a78bfa',
    // Default folder
    _folder: '#45a29e',
    // Default file
    _other: '#64748b'
};

function getFileExt(filename) {
    const parts = filename.split('.');
    if (parts.length > 1) {
        return parts[parts.length - 1].toLowerCase();
    }
    return '';
}

function getItemColor(item) {
    if (item.isDir) return extColorPalette._folder;
    const ext = getFileExt(item.name);
    return extColorPalette[ext] || extColorPalette._other;
}

function formatBytes(bytes, decimals = 1) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

// -------------------------------------------------------------
// Falling Rain Simulation Canvas
// -------------------------------------------------------------
class Circle {
    constructor(fileStat) {
        this.name = fileStat.name;
        this.size = fileStat.size;
        
        const safeSize = Math.max(1, this.size);
        this.radius = Math.max(1.5, Math.min(22, Math.log10(safeSize) * 2.8));
        
        this.x = Math.random() * (width || window.innerWidth);
        this.y = -this.radius - (Math.random() * 80);
        
        this.vy = Math.max(0.6, 2.2 - this.radius / 14) * (Math.random() * 0.4 + 0.8);
        this.color = rainColors[Math.floor(Math.random() * rainColors.length)];
        this.alpha = 1;
        this.showText = Math.random() < 0.08;
    }

    update() {
        this.y += this.vy;
        
        for (let other of circles) {
            if (other === this) continue;
            let dx = this.x - other.x;
            let dy = this.y - other.y;
            let distSq = dx * dx + dy * dy;
            let minDist = this.radius + other.radius + 2;
            
            if (distSq < minDist * minDist) {
                let dist = Math.sqrt(distSq) || 0.1;
                let overlap = minDist - dist;
                let nx = dx / dist;
                this.x += nx * overlap * 0.05;
                other.x -= nx * overlap * 0.05;
            }
        }
        
        const dissolveStart = height * 0.82;
        if (this.y > dissolveStart) {
            let pct = (this.y - dissolveStart) / (height - dissolveStart);
            this.alpha = Math.max(0, 1 - pct);
        }
    }

    draw(ctx) {
        if (this.alpha <= 0) return;
        
        const drawX = Math.round(this.x);
        const drawY = Math.round(this.y);

        ctx.globalAlpha = this.alpha;
        ctx.beginPath();
        ctx.arc(drawX, drawY, this.radius, 0, Math.PI * 2);
        ctx.fillStyle = this.color;
        ctx.fill();
        
        if (this.showText && this.radius > 4) {
            ctx.fillStyle = '#ffffff';
            ctx.font = '600 10px "Segoe UI", -apple-system, sans-serif';
            ctx.textAlign = 'center';
            ctx.fillText(this.name, drawX, Math.round(drawY - this.radius - 4));
        }
        ctx.globalAlpha = 1;
    }
}

function animate() {
    // Clear canvas completely each frame to eliminate all motion blur and ghost trails
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = '#0b0c10';
    ctx.fillRect(0, 0, width, height);
    
    for (let i = circles.length - 1; i >= 0; i--) {
        let c = circles[i];
        c.update();
        c.draw(ctx);
        if (c.alpha <= 0 || c.y > height + c.radius) {
            circles.splice(i, 1);
        }
    }
    requestAnimationFrame(animate);
}

// -------------------------------------------------------------
// Results Dashboard Rendering
// -------------------------------------------------------------

async function renderResultsView(view) {
    if (!view) return;
    currentFolderView = view;

    const container = document.getElementById('treemap-container');
    container.style.display = 'flex'; // Ensure container is visible

    // 1. Update Header Stats
    document.getElementById('wds-total-size').innerText = `Total: ${formatBytes(view.size)}`;
    const items = view.children || [];
    document.getElementById('wds-item-count').innerText = `${items.length} items`;
    document.getElementById('wds-dir-count').innerText = `${items.length} items`;

    // 2. Up Button state
    const upBtn = document.getElementById('wds-up-btn');
    if (view.parent && view.parent !== "") {
        upBtn.disabled = false;
        upBtn.onclick = async () => {
            const pView = await window.go.main.App.GetFolderView(view.parent);
            if (pView) renderResultsView(pView);
        };
    } else {
        upBtn.disabled = true;
    }

    // 3. Breadcrumbs
    renderBreadcrumbs(view.path);

    // 4. Directory Contents Table
    renderTable(items, view.size);

    // 5. Extension Breakdown Legend
    renderExtensionLegend(items);

    // 6. Cushion Treemap with ECharts
    renderTreemap(items, view.size);
}

function renderBreadcrumbs(pathStr) {
    const bcContainer = document.getElementById('wds-breadcrumbs');
    bcContainer.innerHTML = '';

    if (!pathStr) return;
    // Split on either / or \
    const segments = pathStr.split(/[/\\]+/).filter(Boolean);
    
    let accumulatedPath = '';
    const isWindows = pathStr.includes(':');

    segments.forEach((seg, idx) => {
        if (idx === 0 && isWindows) {
            accumulatedPath = seg + '\\';
        } else {
            accumulatedPath = accumulatedPath ? (accumulatedPath.replace(/[/\\]+$/, '') + '\\' + seg) : seg;
        }

        const crumb = document.createElement('span');
        crumb.className = 'wds-crumb-item' + (idx === segments.length - 1 ? ' active' : '');
        crumb.innerText = seg;
        const targetPath = accumulatedPath;

        crumb.onclick = async () => {
            if (idx !== segments.length - 1) {
                const targetView = await window.go.main.App.GetFolderView(targetPath);
                if (targetView) renderResultsView(targetView);
            }
        };

        bcContainer.appendChild(crumb);

        if (idx < segments.length - 1) {
            const sep = document.createElement('span');
            sep.className = 'wds-crumb-sep';
            sep.innerText = '>';
            bcContainer.appendChild(sep);
        }
    });
}

function renderTable(items, totalFolderSize) {
    const tbody = document.getElementById('wds-table-body');
    tbody.innerHTML = '';

    // Sort items by size descending
    const sorted = [...items].sort((a, b) => b.size - a.size);

    sorted.forEach(item => {
        const tr = document.createElement('tr');
        if (item.isDir) {
            tr.className = 'is-dir';
            tr.onclick = async () => {
                const childView = await window.go.main.App.GetFolderView(item.path);
                if (childView) renderResultsView(childView);
            };
        }

        const pct = totalFolderSize > 0 ? ((item.size / totalFolderSize) * 100) : 0;
        const color = getItemColor(item);
        const ext = item.isDir ? 'Folder' : (getFileExt(item.name).toUpperCase() || 'FILE');

        tr.innerHTML = `
            <td>
                <div class="row-name-col" title="${item.name}">
                    <span class="row-icon">${item.isDir ? '📁' : '📄'}</span>
                    <span class="row-text">${item.name}</span>
                </div>
            </td>
            <td>
                <div class="wds-bar-wrapper">
                    <div class="wds-bar-fill" style="width: ${Math.min(100, Math.max(1, pct))}%; background: ${color};"></div>
                    <span class="wds-bar-text">${pct.toFixed(1)}%</span>
                </div>
            </td>
            <td class="size-mono">${formatBytes(item.size)}</td>
            <td>
                <span class="type-pill" style="background: ${color}22; color: ${color}; border: 1px solid ${color}44;">
                    ${ext}
                </span>
            </td>
        `;
        tbody.appendChild(tr);
    });
}

function renderExtensionLegend(items) {
    const extContainer = document.getElementById('wds-ext-list');
    extContainer.innerHTML = '';

    // Tally size and count by extension
    const extMap = {};
    let totalFilesSize = 0;

    items.forEach(item => {
        if (item.isDir) {
            extMap['_DIR'] = extMap['_DIR'] || { name: 'Folders', size: 0, count: 0, color: extColorPalette._folder };
            extMap['_DIR'].size += item.size;
            extMap['_DIR'].count += 1;
        } else {
            const ext = getFileExt(item.name).toLowerCase() || 'unknown';
            const color = extColorPalette[ext] || extColorPalette._other;
            extMap[ext] = extMap[ext] || { name: `.${ext}`, size: 0, count: 0, color: color };
            extMap[ext].size += item.size;
            extMap[ext].count += 1;
            totalFilesSize += item.size;
        }
    });

    const extList = Object.values(extMap).sort((a, b) => b.size - a.size);

    extList.forEach(ext => {
        const card = document.createElement('div');
        card.className = 'ext-card';
        card.innerHTML = `
            <div class="ext-left">
                <div class="ext-color-dot" style="background: ${ext.color};"></div>
                <span class="ext-name">${ext.name}</span>
            </div>
            <div class="ext-right">
                <span>${formatBytes(ext.size)}</span>
                <span class="ext-size">${ext.count} ${ext.count === 1 ? 'item' : 'items'}</span>
            </div>
        `;
        extContainer.appendChild(card);
    });
}

function renderTreemap(items, totalSize) {
    const chartDiv = document.getElementById('wds-chart');
    if (!chartDiv) return;

    // Small threshold (0.1%) for visual grouping
    const threshold = totalSize / 1000;
    const data = [];
    let otherSize = 0;

    const sorted = [...items].sort((a, b) => b.size - a.size);

    sorted.forEach(item => {
        if (item.size < threshold) {
            otherSize += item.size;
        } else {
            const color = getItemColor(item);
            data.push({
                name: item.name,
                value: item.size,
                path: item.path,
                isDir: item.isDir,
                itemStyle: {
                    color: color,
                    borderColor: '#06070a',
                    borderWidth: 2,
                    gapWidth: 2
                }
            });
        }
    });

    if (otherSize > 0) {
        data.push({
            name: "Other (Small Items)",
            value: otherSize,
            path: "",
            isDir: false,
            itemStyle: {
                color: '#334155',
                borderColor: '#06070a',
                borderWidth: 2
            }
        });
    }

    // Schedule ECharts init/update on next frame after DOM has layout
    requestAnimationFrame(() => {
        if (!echartsInstance) {
            echartsInstance = echarts.init(chartDiv, 'dark');
            
            echartsInstance.on('click', async (params) => {
                if (params.data && params.data.isDir && params.data.path) {
                    const childView = await window.go.main.App.GetFolderView(params.data.path);
                    if (childView) renderResultsView(childView);
                }
            });
        }

        const option = {
            backgroundColor: 'transparent',
            tooltip: {
                backgroundColor: 'rgba(15, 17, 26, 0.95)',
                borderColor: 'rgba(102, 252, 241, 0.4)',
                borderWidth: 1,
                textStyle: { color: '#e2e8f0', fontSize: 12 },
                formatter: function (info) {
                    const isDir = info.data.isDir;
                    const icon = isDir ? '📁' : '📄';
                    const formatted = formatBytes(info.value);
                    const pct = totalSize > 0 ? ((info.value / totalSize) * 100).toFixed(1) : 0;
                    return `
                        <div style="font-weight: 700; margin-bottom: 4px;">${icon} ${info.name}</div>
                        <div>Size: <span style="color: #66fcf1; font-family: monospace;">${formatted}</span> (${pct}%)</div>
                        ${isDir ? '<div style="color: #94a3b8; font-size: 11px; margin-top: 4px;">👉 Click to zoom into folder</div>' : ''}
                    `;
                }
            },
            series: [{
                type: 'treemap',
                data: data,
                roam: false,
                nodeClick: false,
                breadcrumb: { show: false },
                levels: [
                    {
                        itemStyle: {
                            borderColor: '#06070a',
                            borderWidth: 2,
                            gapWidth: 2
                        }
                    }
                ],
                label: {
                    show: true,
                    formatter: function (params) {
                        return `${params.name}\n${formatBytes(params.value)}`;
                    },
                    color: '#ffffff',
                    fontSize: 11,
                    fontWeight: 600,
                    textShadowColor: 'rgba(0, 0, 0, 0.9)',
                    textShadowBlur: 3
                }
            }]
        };

        echartsInstance.setOption(option, true);
        echartsInstance.resize();
    });
}

// -------------------------------------------------------------
// App Initialization & Resize Observer
// -------------------------------------------------------------
window.onload = function() {
    canvas = document.getElementById('canvas');
    ctx = canvas.getContext('2d');
    
    function resizeCanvas() {
        width = window.innerWidth || document.documentElement.clientWidth || document.body.clientWidth;
        height = window.innerHeight || document.documentElement.clientHeight || document.body.clientHeight;
        
        const dpr = window.devicePixelRatio || 1;
        canvas.width = Math.floor(width * dpr);
        canvas.height = Math.floor(height * dpr);
        
        // Use setTransform to guarantee exact DPR scale without cumulative distortion
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
        canvas.style.width = width + 'px';
        canvas.style.height = height + 'px';

        // Re-distribute any circles that fall outside new boundaries
        for (let c of circles) {
            if (c.x > width) {
                c.x = Math.random() * width;
            }
        }
    }
    
    window.addEventListener('resize', () => {
        resizeCanvas();
        if (echartsInstance) {
            echartsInstance.resize();
        }
    });

    window.addEventListener('orientationchange', () => {
        setTimeout(resizeCanvas, 100);
    });
    
    // Resize observer on treemap wrapper to handle split pane resizes
    const chartWrapper = document.getElementById('wds-chart-wrapper');
    if (chartWrapper && window.ResizeObserver) {
        const ro = new ResizeObserver(() => {
            if (echartsInstance) {
                echartsInstance.resize();
            }
        });
        ro.observe(chartWrapper);
    }

    resizeCanvas();
    animate();

    const scanBtn = document.getElementById('scan-btn');
    const resultsBtn = document.getElementById('results-btn');
    const uiLayer = document.getElementById('ui-layer');
    const statusText = document.getElementById('status-text');
    const closeBtn = document.getElementById('wds-close-btn');
    const tmContainer = document.getElementById('treemap-container');
    const speedInput = document.getElementById('speed-select');

    // Segmented Speed Control
    const segButtons = document.querySelectorAll('.seg-btn');
    segButtons.forEach(btn => {
        btn.addEventListener('click', () => {
            segButtons.forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            if (speedInput) {
                speedInput.value = btn.getAttribute('data-value');
            }
        });
    });

    scanBtn.addEventListener('click', async () => {
        statusText.innerText = "Selecting directory...";
        const speed = document.getElementById('speed-select').value;
        try {
            const dir = await window.go.main.App.SelectDirectory(speed);
            if (dir) {
                circles = [];
                resultsBtn.style.display = 'none';
                uiLayer.classList.add('scanning');
                statusText.innerText = `Scanning: ${dir}`;
            } else {
                statusText.innerText = "Selection cancelled.";
            }
        } catch (e) {
            console.error(e);
            statusText.innerText = "Error calling Go backend.";
        }
    });

    resultsBtn.addEventListener('click', async () => {
        statusText.innerText = "Loading results...";
        try {
            const rootData = await window.go.main.App.GetRootFolderView();
            if (rootData) {
                uiLayer.style.display = 'none';
                renderResultsView(rootData);
            } else {
                statusText.innerText = "No tree data available.";
            }
        } catch (e) {
            console.error(e);
            statusText.innerText = "Failed to load results.";
        }
    });

    closeBtn.addEventListener('click', () => {
        tmContainer.style.display = 'none';
        uiLayer.style.display = 'block';
        resizeCanvas();
    });

    // Export Dropdown & Report Handlers
    const exportDropdown = document.getElementById('wds-export-dropdown');
    const exportBtn = document.getElementById('wds-export-btn');
    const exportHtmlBtn = document.getElementById('export-html-btn');

    if (exportBtn && exportDropdown) {
        exportBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            exportDropdown.classList.toggle('open');
        });

        document.addEventListener('click', (e) => {
            if (!exportDropdown.contains(e.target)) {
                exportDropdown.classList.remove('open');
            }
        });
    }

    if (exportHtmlBtn) {
        exportHtmlBtn.addEventListener('click', async () => {
            exportDropdown.classList.remove('open');
            try {
                const savedPath = await window.go.main.App.ExportHTMLReport();
                if (savedPath) {
                    showToast(`📄 Report saved to: ${savedPath}`);
                }
            } catch (err) {
                console.error("Export error:", err);
                showToast(`❌ Export failed: ${err}`, true);
            }
        });
    }

    function showToast(message, isError = false) {
        const existing = document.querySelector('.toast-notification');
        if (existing) existing.remove();

        const toast = document.createElement('div');
        toast.className = 'toast-notification';
        if (isError) {
            toast.style.borderColor = 'var(--accent-pink)';
            toast.style.boxShadow = '0 8px 30px rgba(0,0,0,0.6), 0 0 20px rgba(244, 63, 94, 0.3)';
        }
        toast.innerText = message;
        document.body.appendChild(toast);

        setTimeout(() => {
            toast.style.opacity = '0';
            toast.style.transform = 'translateY(10px)';
            toast.style.transition = 'all 0.3s ease';
            setTimeout(() => toast.remove(), 300);
        }, 4500);
    }

    // Dynamic Saturation & Adaptive Rate Limiter for Rain
    const TARGET_SATURATION = 100;
    const HARD_CAP = 250;

    window.runtime.EventsOn("files-scanned", (files) => {
        if (!files || !files.length) return;

        for (let i = 0; i < files.length; i++) {
            const activeCount = circles.length;

            if (activeCount >= HARD_CAP) {
                break;
            }

            if (activeCount < TARGET_SATURATION) {
                // Under saturation: 100% admission for instantaneous feedback on start or small scans
                circles.push(new Circle(files[i]));
            } else {
                // Saturated: dynamically scale down admission rate to maintain comfortable density
                const saturationRatio = activeCount / TARGET_SATURATION;
                const admissionProbability = Math.max(0.04, 0.4 / (saturationRatio * saturationRatio));

                if (Math.random() < admissionProbability) {
                    circles.push(new Circle(files[i]));
                }
            }
        }
    });

    window.runtime.EventsOn("scan-complete", () => {
        setTimeout(() => {
            uiLayer.classList.remove('scanning');
            statusText.innerText = "Scan complete!";
            resultsBtn.style.display = 'inline-flex';
        }, 1200);
    });
};
