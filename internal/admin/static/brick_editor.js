(function() {
    'use strict';

    var CELL = 16;
    var SCALE = 25;
    var DEFAULT_BORDER = {
        top:    [{x:0,y:16},{x:16,y:16}],
        right:  [{x:16,y:16},{x:16,y:0}],
        bottom: [{x:16,y:0},{x:0,y:0}],
        left:   [{x:0,y:0},{x:0,y:16}]
    };

    function BrickEditor() {
        this.canvas = document.getElementById('brickCanvas');
        this.ctx = this.canvas.getContext('2d');
        this.previewCanvas = document.getElementById('previewCanvas');
        this.previewCtx = this.previewCanvas.getContext('2d');

        this.border = JSON.parse(JSON.stringify(BORDER_DATA || DEFAULT_BORDER));
        this.activeEdge = 'top';
        this.dragIndex = -1;

        this.init();
    }

    BrickEditor.prototype.init = function() {
        var self = this;
        this.canvas.addEventListener('mousedown', function(e) { self.onMouseDown(e); });
        this.canvas.addEventListener('mousemove', function(e) { self.onMouseMove(e); });
        this.canvas.addEventListener('mouseup', function(e) { self.onMouseUp(e); });
        this.canvas.addEventListener('dblclick', function(e) { self.onDblClick(e); });
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.screenToGrid = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;
        return {
            x: Math.round(sx / SCALE * 2) / 2,
            y: Math.round((CELL - sy / SCALE) * 2) / 2
        };
    };

    BrickEditor.prototype.gridToScreen = function(pt) {
        return {
            x: pt.x * SCALE,
            y: (CELL - pt.y) * SCALE
        };
    };

    BrickEditor.prototype.findNearestPoint = function(gx, gy) {
        var points = this.border[this.activeEdge];
        var best = -1, bestDist = Infinity;
        for (var i = 0; i < points.length; i++) {
            var dx = points[i].x - gx;
            var dy = points[i].y - gy;
            var d = dx * dx + dy * dy;
            if (d < bestDist && d < 4) {
                bestDist = d;
                best = i;
            }
        }
        return best;
    };

    BrickEditor.prototype.onMouseDown = function(e) {
        var g = this.screenToGrid(e);
        var idx = this.findNearestPoint(g.x, g.y);
        if (idx >= 0) {
            this.dragIndex = idx;
        }
    };

    BrickEditor.prototype.onMouseMove = function(e) {
        if (this.dragIndex >= 0) {
            var g = this.screenToGrid(e);
            g.x = Math.max(0, Math.min(CELL, g.x));
            g.y = Math.max(0, Math.min(CELL, g.y));
            this.border[this.activeEdge][this.dragIndex] = g;
            this.renderPointList();
            this.render();
        }
    };

    BrickEditor.prototype.onMouseUp = function() {
        this.dragIndex = -1;
    };

    BrickEditor.prototype.onDblClick = function(e) {
        var g = this.screenToGrid(e);
        g.x = Math.max(0, Math.min(CELL, g.x));
        g.y = Math.max(0, Math.min(CELL, g.y));

        var idx = this.findNearestPoint(g.x, g.y);
        var points = this.border[this.activeEdge];
        if (idx >= 0 && points.length > 2) {
            points.splice(idx, 1);
        } else if (idx < 0) {
            var bestInsert = points.length - 1;
            var bestDist = Infinity;
            for (var i = 0; i < points.length - 1; i++) {
                var mx = (points[i].x + points[i+1].x) / 2;
                var my = (points[i].y + points[i+1].y) / 2;
                var d = (g.x - mx) * (g.x - mx) + (g.y - my) * (g.y - my);
                if (d < bestDist) {
                    bestDist = d;
                    bestInsert = i + 1;
                }
            }
            points.splice(bestInsert, 0, g);
        }
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.setEdge = function(edge, btn) {
        this.activeEdge = edge;
        this.dragIndex = -1;
        document.querySelectorAll('.edge-btn').forEach(function(b) { b.classList.remove('active'); });
        if (btn) btn.classList.add('active');
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.resetEdge = function() {
        this.border[this.activeEdge] = JSON.parse(JSON.stringify(DEFAULT_BORDER[this.activeEdge]));
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.renderPointList = function() {
        var list = document.getElementById('pointList');
        var points = this.border[this.activeEdge];
        var self = this;
        list.innerHTML = '';
        points.forEach(function(p, i) {
            var div = document.createElement('div');
            div.className = 'point-item';
            div.innerHTML = '<span>(' + p.x + ', ' + p.y + ')</span>';
            if (points.length > 2) {
                var btn = document.createElement('button');
                btn.className = 'btn btn-danger btn-sm';
                btn.style.cssText = 'padding:1px 5px;font-size:10px';
                btn.textContent = 'X';
                btn.onclick = function() {
                    points.splice(i, 1);
                    self.renderPointList();
                    self.render();
                };
                div.appendChild(btn);
            }
            list.appendChild(div);
        });
    };

    BrickEditor.prototype.render = function() {
        var ctx = this.ctx;
        var w = this.canvas.width;
        var h = this.canvas.height;

        ctx.clearRect(0, 0, w, h);

        // Background grid
        ctx.fillStyle = '#f0f0f0';
        ctx.fillRect(0, 0, w, h);
        ctx.strokeStyle = '#ddd';
        ctx.lineWidth = 1;
        for (var i = 0; i <= CELL; i++) {
            ctx.beginPath();
            ctx.moveTo(i * SCALE, 0);
            ctx.lineTo(i * SCALE, h);
            ctx.stroke();
            ctx.beginPath();
            ctx.moveTo(0, i * SCALE);
            ctx.lineTo(w, i * SCALE);
            ctx.stroke();
        }

        // Draw filled polygon
        var color = document.getElementById('propColor').value || '#8B4513';
        ctx.fillStyle = color + '44';
        ctx.beginPath();
        var edges = ['bottom', 'right', 'top', 'left'];
        var first = true;
        for (var e = 0; e < edges.length; e++) {
            var pts = this.border[edges[e]];
            for (var p = 0; p < pts.length; p++) {
                var s = this.gridToScreen(pts[p]);
                if (first) { ctx.moveTo(s.x, s.y); first = false; }
                else ctx.lineTo(s.x, s.y);
            }
        }
        ctx.closePath();
        ctx.fill();

        // Draw all edges
        var edgeColors = {top: '#e74c3c', right: '#27ae60', bottom: '#3498db', left: '#f39c12'};
        for (var e = 0; e < edges.length; e++) {
            var edge = edges[e];
            var pts = this.border[edge];
            ctx.strokeStyle = edge === this.activeEdge ? edgeColors[edge] : '#999';
            ctx.lineWidth = edge === this.activeEdge ? 3 : 1;
            ctx.beginPath();
            for (var p = 0; p < pts.length; p++) {
                var s = this.gridToScreen(pts[p]);
                if (p === 0) ctx.moveTo(s.x, s.y);
                else ctx.lineTo(s.x, s.y);
            }
            ctx.stroke();

            if (edge === this.activeEdge) {
                for (var p = 0; p < pts.length; p++) {
                    var s = this.gridToScreen(pts[p]);
                    ctx.fillStyle = edgeColors[edge];
                    ctx.beginPath();
                    ctx.arc(s.x, s.y, 5, 0, Math.PI * 2);
                    ctx.fill();
                    ctx.fillStyle = '#fff';
                    ctx.font = '10px monospace';
                    ctx.fillText(p, s.x + 7, s.y - 5);
                }
            }
        }

        // Axis labels
        ctx.fillStyle = '#999';
        ctx.font = '10px monospace';
        ctx.fillText('(0,0)', 2, h - 4);
        ctx.fillText('x\u2192', w / 2, h - 4);
        ctx.fillText('y\u2191', 2, h / 2);

        this.renderPreview();
    };

    BrickEditor.prototype.renderPreview = function() {
        var ctx = this.previewCtx;
        var size = 48;
        ctx.clearRect(0, 0, size, size);

        var color = document.getElementById('propColor').value || '#8B4513';
        var offset = (size - CELL) / 2;

        ctx.fillStyle = color;
        ctx.beginPath();
        var edges = ['bottom', 'right', 'top', 'left'];
        var first = true;
        for (var e = 0; e < edges.length; e++) {
            var pts = this.border[edges[e]];
            for (var p = 0; p < pts.length; p++) {
                var px = offset + pts[p].x;
                var py = offset + (CELL - pts[p].y);
                if (first) { ctx.moveTo(px, py); first = false; }
                else ctx.lineTo(px, py);
            }
        }
        ctx.closePath();
        ctx.fill();
        ctx.strokeStyle = '#333';
        ctx.lineWidth = 0.5;
        ctx.stroke();
    };

    BrickEditor.prototype.save = function() {
        var data = {
            brickTypeId: BRICK_ID || 0,
            name: document.getElementById('propName').value,
            imageId: document.getElementById('propImageId').value,
            destructible: document.getElementById('propDestructible').checked,
            border: this.border,
            color: document.getElementById('propColor').value
        };

        fetch('/brick-types/save', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(data)
        })
        .then(function(r) { return r.json(); })
        .then(function(res) {
            if (res.ok) {
                window.location.href = '/brick-types?flash=Saved+successfully';
            } else {
                alert('Save failed: ' + (res.error || 'unknown'));
            }
        });
    };

    window.brickEditor = new BrickEditor();
})();
