let canvas, ctx;
let circles = [];
let width, height;

// Color palette for circles
const colors = [
    '#66fcf1', '#45a29e', '#ff0055', '#00ffaa', '#ffaa00', '#aa00ff'
];

class Circle {
    constructor(fileStat) {
        this.name = fileStat.name;
        this.size = fileStat.size;
        
        // Map size to radius - made smaller and logarithmic
        let safeSize = Math.max(1, this.size);
        this.radius = Math.max(1.5, Math.min(25, Math.log10(safeSize) * 3));
        
        this.x = Math.random() * width;
        this.y = -this.radius - (Math.random() * 100); 
        
        // Speed inversely proportional to size (slower falling for rain vibe)
        this.vy = Math.max(0.5, 2 - this.radius / 15) * (Math.random() * 0.4 + 0.6);
        
        this.color = colors[Math.floor(Math.random() * colors.length)];
        this.alpha = 1;
        
        // 10% chance to show filename
        this.showText = Math.random() < 0.1;
    }

    update() {
        this.y += this.vy;
        
        // Soft Repulsion to prevent overlaps
        for (let other of circles) {
            if (other === this) continue;
            let dx = this.x - other.x;
            let dy = this.y - other.y;
            let distSq = dx*dx + dy*dy;
            let minDist = this.radius + other.radius + 2;
            
            if (distSq < minDist * minDist) {
                let dist = Math.sqrt(distSq);
                if (dist === 0) dist = 0.1;
                let overlap = minDist - dist;
                let nx = dx / dist;
                
                // Gently nudge horizontally to slide past
                this.x += nx * overlap * 0.05;
                other.x -= nx * overlap * 0.05;
            }
        }
        
        // Dissolve near bottom
        let dissolveStart = height * 0.8;
        if (this.y > dissolveStart) {
            let pct = (this.y - dissolveStart) / (height - dissolveStart);
            this.alpha = Math.max(0, 1 - pct);
        }
    }

    draw(ctx) {
        if (this.alpha <= 0) return;
        
        ctx.globalAlpha = this.alpha;
        ctx.beginPath();
        ctx.arc(this.x, this.y, this.radius, 0, Math.PI * 2);
        
        // Reduced glow for crispness
        ctx.shadowBlur = 2;
        ctx.shadowColor = this.color;
        ctx.fillStyle = this.color;
        ctx.fill();
        
        // Reset shadow for text
        ctx.shadowBlur = 0;
        
        if (this.showText && this.radius > 3) {
            ctx.fillStyle = 'rgba(255, 255, 255, 0.9)';
            ctx.font = '10px Segoe UI';
            ctx.textAlign = 'center';
            ctx.fillText(this.name, this.x, this.y - this.radius - 4);
        }
        ctx.globalAlpha = 1;
    }
}

function animate() {
    // Clear with slight trail effect
    ctx.fillStyle = 'rgba(11, 12, 16, 0.4)';
    ctx.fillRect(0, 0, width, height);
    
    // Update and draw circles
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

window.onload = function() {
    canvas = document.getElementById('canvas');
    ctx = canvas.getContext('2d');
    
    // Handle High DPI displays for crisp rendering
    function resize() {
        width = window.innerWidth;
        height = window.innerHeight;
        
        const dpr = window.devicePixelRatio || 1;
        canvas.width = width * dpr;
        canvas.height = height * dpr;
        
        ctx.scale(dpr, dpr);
        canvas.style.width = width + 'px';
        canvas.style.height = height + 'px';
    }
    
    window.addEventListener('resize', resize);
    resize();
    
    animate();

    const scanBtn = document.getElementById('scan-btn');
    const uiLayer = document.getElementById('ui-layer');
    const statusText = document.getElementById('status-text');

    scanBtn.addEventListener('click', async () => {
        statusText.innerText = "Selecting directory...";
        const speed = document.getElementById('speed-select').value;
        try {
            const dir = await window.go.main.App.SelectDirectory(speed);
            if (dir) {
                circles = []; // Clear old circles from previous scan
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

    // Listen to Wails events
    window.runtime.EventsOn("files-scanned", (files) => {
        files.forEach(f => {
            circles.push(new Circle(f));
        });
    });

    window.runtime.EventsOn("scan-complete", () => {
        setTimeout(() => {
            uiLayer.classList.remove('scanning');
            statusText.innerText = "Scan complete!";
        }, 3000);
    });
};
