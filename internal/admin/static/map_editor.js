// Map Editor - Canvas-based tilemap editor with polygon brick rendering
(function() {
    'use strict';

    // Build brick lookup maps
    var BRICK_COLORS = {};
    var BRICK_BORDERS = {};
    BRICK_TYPES.forEach(function(bt) {
        BRICK_COLORS[bt.BrickTypeID] = bt.Color || '#888';
        if (bt.Border) {
            var border = typeof bt.Border === 'string' ? JSON.parse(bt.Border) : bt.Border;
            if (border && border.top && border.top.length > 0) {
                BRICK_BORDERS[bt.BrickTypeID] = border;
            }
        }
    });

    function buildPolygonPoints(border, cellSize, offsetX, offsetY) {
        var scale = cellSize / 16;
        var points = [];
        var edges = ['bottom', 'right', 'top', 'left'];
        for (var i = 0; i < edges.length; i++) {
            var edge = border[edges[i]];
            if (!edge) continue;
            for (var j = 0; j < edge.length; j++) {
                if (j === edge.length - 1 && i < edges.length - 1) continue;
                points.push({
                    x: offsetX + edge[j].x * scale,
                    y: offsetY + (16 - edge[j].y) * scale
                });
            }
        }
        return points;
    }

    function drawBrickPolygon(ctx, border, cellSize, offsetX, offsetY, color) {
        var points = buildPolygonPoints(border, cellSize, offsetX, offsetY);
        if (points.length < 3) return;

        ctx.beginPath();
        ctx.moveTo(points[0].x, points[0].y);
        for (var i = 1; i < points.length; i++) {
            ctx.lineTo(points[i].x, points[i].y);
        }
        ctx.closePath();
        ctx.fillStyle = color;
        ctx.fill();
        ctx.strokeStyle = darkenColor(color, 0.3);
        ctx.lineWidth = 0.5;
        ctx.stroke();
    }

    function darkenColor(hex, amount) {
        hex = hex.replace('#', '');
        if (hex.length === 3) hex = hex[0]+hex[0]+hex[1]+hex[1]+hex[2]+hex[2];
        var r = Math.max(0, parseInt(hex.substr(0, 2), 16) * (1 - amount));
        var g = Math.max(0, parseInt(hex.substr(2, 2), 16) * (1 - amount));
        var b = Math.max(0, parseInt(hex.substr(4, 2), 16) * (1 - amount));
        return 'rgb(' + Math.round(r) + ',' + Math.round(g) + ',' + Math.round(b) + ')';
    }

    function drawPalettePreview(canvas, border, color) {
        var ctx = canvas.getContext('2d');
        var size = canvas.width;
        canvas.height = size;
        ctx.clearRect(0, 0, size, size);
        if (border) {
            drawBrickPolygon(ctx, border, size, 0, 0, color);
        } else {
            ctx.fillStyle = color;
            ctx.fillRect(0, 0, size, size);
        }
    }

    function MapEditor(canvasId, wrapId) {
        this.canvas = document.getElementById(canvasId);
        this.ctx = this.canvas.getContext('2d');
        this.wrap = document.getElementById(wrapId);

        this.gridWidth = 100;
        this.gridHeight = 56;
        this.cellSize = 16;
        this.tiles = [];
        this.spawnPoints = [];

        this.tool = 'paint';
        this.selectedBrick = BRICK_TYPES.length > 0 ? BRICK_TYPES[0].BrickTypeID : null;
        this.showGrid = true;
        this.zoom = 1;
        this.panX = 0;
        this.panY = 0;
        this.isPanning = false;
        this.isDrawing = false;
        this.lastPanX = 0;
        this.lastPanY = 0;
        this.spaceHeld = false;

        this.selection = null;
        this.selectionTiles = null;
        this.isSelecting = false;
        this.isDraggingSelection = false;
        this.dragOffsetCol = 0;
        this.dragOffsetRow = 0;

        this.undoStack = [];
        this.redoStack = [];
        this.currentStroke = null;

        this.init();
    }

    MapEditor.prototype.init = function() {
        var self = this;
        this.buildPalette();
        this.resizeCanvas();

        if (IS_NEW) {
            this.initNewMap();
        } else {
            this.load();
        }

        window.addEventListener('resize', function() { self.resizeCanvas(); self.render(); });

        this.canvas.addEventListener('mousedown', function(e) { self.onMouseDown(e); });
        this.canvas.addEventListener('mousemove', function(e) { self.onMouseMove(e); });
        this.canvas.addEventListener('mouseup', function(e) { self.onMouseUp(e); });
        this.canvas.addEventListener('wheel', function(e) { self.onWheel(e); e.preventDefault(); }, {passive: false});
        this.canvas.addEventListener('contextmenu', function(e) { e.preventDefault(); });

        document.addEventListener('keydown', function(e) {
            if (e.code === 'Space') { self.spaceHeld = true; e.preventDefault(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'z') { e.preventDefault(); self.undo(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'y') { e.preventDefault(); self.redo(); }
        });
        document.addEventListener('keyup', function(e) {
            if (e.code === 'Space') { self.spaceHeld = false; }
        });
    };

    MapEditor.prototype.initNewMap = function() {
        this.gridWidth = 100;
        this.gridHeight = 56;
        this.cellSize = 16;
        this.tiles = [];
        for (var row = 0; row < this.gridHeight; row++) {
            this.tiles.push(new Array(this.gridWidth).fill(0));
        }
        this.spawnPoints = [];
        this.updateFormFromState();
        this.fitView();
        this.renderSpawnList();
        this.render();
    };

    MapEditor.prototype.buildPalette = function() {
        var palette = document.getElementById('brickPalette');
        var self = this;
        BRICK_TYPES.forEach(function(bt) {
            var id = bt.BrickTypeID;
            var btn = document.createElement('div');
            btn.className = 'brick-btn' + (id === self.selectedBrick ? ' active' : '');
            btn.title = bt.Name + (bt.Destructible ? ' (D)' : '');
            btn.setAttribute('data-id', id);

            var cvs = document.createElement('canvas');
            cvs.width = 32;
            cvs.height = 32;
            var border = BRICK_BORDERS[id] || null;
            var color = BRICK_COLORS[id];
            drawPalettePreview(cvs, border, color);
            btn.appendChild(cvs);

            btn.onclick = function() {
                document.querySelectorAll('.brick-btn').forEach(function(b) { b.classList.remove('active'); });
                btn.classList.add('active');
                self.selectedBrick = id;
            };
            palette.appendChild(btn);
        });
    };

    MapEditor.prototype.resizeCanvas = function() {
        this.canvas.width = this.wrap.clientWidth;
        this.canvas.height = this.wrap.clientHeight;
    };

    MapEditor.prototype.load = function() {
        var self = this;
        fetch('/api/maps/tiles?id=' + MAP_ID)
            .then(function(r) { return r.json(); })
            .then(function(data) {
                self.gridWidth = data.gridWidth;
                self.gridHeight = data.gridHeight;
                self.cellSize = data.cellSize;

                if (data.tiles && data.tiles.length > 0) {
                    self.tiles = data.tiles;
                } else {
                    self.tiles = [];
                    for (var row = 0; row < self.gridHeight; row++) {
                        self.tiles.push(new Array(self.gridWidth).fill(0));
                    }
                }

                // Normalize spawn points: handle both {x,y} and {X,Y}
                self.spawnPoints = [];
                var rawSpawns = data.spawnPoints || [];
                for (var i = 0; i < rawSpawns.length; i++) {
                    var sp = rawSpawns[i];
                    self.spawnPoints.push({
                        x: sp.x !== undefined ? sp.x : sp.X,
                        y: sp.y !== undefined ? sp.y : sp.Y
                    });
                }

                // Populate form fields
                document.getElementById('propName').value = data.name || '';
                document.getElementById('propGridWidth').value = data.gridWidth;
                document.getElementById('propGridHeight').value = data.gridHeight;
                document.getElementById('propCellSize').value = data.cellSize;
                document.getElementById('propMinRankTier').value = data.minRankTier || 'bronze';
                document.getElementById('propDescription').value = data.description || '';

                var windRange = [0, 4];
                try {
                    if (typeof data.defaultWindPowerRange === 'string') {
                        windRange = JSON.parse(data.defaultWindPowerRange);
                    } else if (Array.isArray(data.defaultWindPowerRange)) {
                        windRange = data.defaultWindPowerRange;
                    }
                } catch(e) {}
                if (Array.isArray(windRange) && windRange.length >= 2) {
                    document.getElementById('propWindMin').value = windRange[0];
                    document.getElementById('propWindMax').value = windRange[1];
                }

                self.fitView();
                self.renderSpawnList();
                self.render();
            });
    };

    MapEditor.prototype.fitView = function() {
        var scaleX = this.canvas.width / (this.gridWidth * this.cellSize);
        var scaleY = this.canvas.height / (this.gridHeight * this.cellSize);
        this.zoom = Math.min(scaleX, scaleY) * 0.9;
        this.panX = (this.canvas.width - this.gridWidth * this.cellSize * this.zoom) / 2;
        this.panY = (this.canvas.height - this.gridHeight * this.cellSize * this.zoom) / 2;
    };

    MapEditor.prototype.updateFormFromState = function() {
        document.getElementById('propName').value = '';
        document.getElementById('propGridWidth').value = this.gridWidth;
        document.getElementById('propGridHeight').value = this.gridHeight;
        document.getElementById('propCellSize').value = this.cellSize;
        document.getElementById('propWindMin').value = 0;
        document.getElementById('propWindMax').value = 4;
        document.getElementById('propMinRankTier').value = 'bronze';
        document.getElementById('propDescription').value = '';
    };

    MapEditor.prototype.save = function() {
        var mapId = IS_NEW ? (document.getElementById('propMapId') ? document.getElementById('propMapId').value.trim() : '') : MAP_ID;
        if (!mapId) {
            alert('Map ID is required');
            return;
        }

        var gridWidth = parseInt(document.getElementById('propGridWidth').value) || 100;
        var gridHeight = parseInt(document.getElementById('propGridHeight').value) || 56;
        var cellSize = parseInt(document.getElementById('propCellSize').value) || 16;

        if (gridWidth !== this.gridWidth || gridHeight !== this.gridHeight) {
            var newTiles = [];
            for (var r = 0; r < gridHeight; r++) {
                var row = [];
                for (var c = 0; c < gridWidth; c++) {
                    row.push((r < this.tiles.length && c < (this.tiles[r] || []).length) ? this.tiles[r][c] : 0);
                }
                newTiles.push(row);
            }
            this.tiles = newTiles;
            this.gridWidth = gridWidth;
            this.gridHeight = gridHeight;
        }
        this.cellSize = cellSize;

        var windMin = parseFloat(document.getElementById('propWindMin').value) || 0;
        var windMax = parseFloat(document.getElementById('propWindMax').value) || 4;

        var body = {
            mapId: mapId,
            name: document.getElementById('propName').value,
            gridWidth: gridWidth,
            gridHeight: gridHeight,
            cellSize: cellSize,
            defaultWindPowerRange: [windMin, windMax],
            minRankTier: document.getElementById('propMinRankTier').value,
            description: document.getElementById('propDescription').value,
            tiles: this.tiles,
            spawnPoints: this.spawnPoints
        };

        var url = '/api/maps/save';
        if (!IS_NEW) {
            url += '?id=' + MAP_ID;
        }

        fetch(url, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body)
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.ok) {
                alert('Saved!');
                if (IS_NEW && data.mapId) {
                    window.location.href = '/maps/editor?id=' + data.mapId;
                }
            } else {
                alert('Save failed: ' + (data.error || 'unknown'));
            }
        });
    };

    MapEditor.prototype.exportYAML = function() {
        if (IS_NEW) { alert('Save the map first before exporting.'); return; }
        window.open('/api/maps/export?id=' + MAP_ID, '_blank');
    };

    MapEditor.prototype.screenToWorld = function(sx, sy) {
        return {
            x: (sx - this.panX) / this.zoom,
            y: (sy - this.panY) / this.zoom
        };
    };

    MapEditor.prototype.screenToCell = function(sx, sy) {
        var w = this.screenToWorld(sx, sy);
        return {
            col: Math.floor(w.x / this.cellSize),
            row: Math.floor(w.y / this.cellSize)
        };
    };

    MapEditor.prototype.inBounds = function(col, row) {
        return col >= 0 && col < this.gridWidth && row >= 0 && row < this.gridHeight;
    };

    MapEditor.prototype.pushUndo = function(changes) {
        if (!changes || Object.keys(changes).length === 0) return;
        this.undoStack.push(changes);
        this.redoStack = [];
    };

    MapEditor.prototype.undo = function() {
        if (this.undoStack.length === 0) return;
        var changes = this.undoStack.pop();
        var redo = {};
        for (var key in changes) {
            var parts = key.split(',');
            var row = parseInt(parts[0]), col = parseInt(parts[1]);
            redo[key] = this.tiles[row][col];
            this.tiles[row][col] = changes[key];
        }
        this.redoStack.push(redo);
        this.render();
    };

    MapEditor.prototype.redo = function() {
        if (this.redoStack.length === 0) return;
        var changes = this.redoStack.pop();
        var undo = {};
        for (var key in changes) {
            var parts = key.split(',');
            var row = parseInt(parts[0]), col = parseInt(parts[1]);
            undo[key] = this.tiles[row][col];
            this.tiles[row][col] = changes[key];
        }
        this.undoStack.push(undo);
        this.render();
    };

    MapEditor.prototype.setTool = function(tool, btn) {
        this.tool = tool;
        this.selection = null;
        this.selectionTiles = null;
        document.querySelectorAll('.tool-btn').forEach(function(b) { b.classList.remove('active'); });
        if (btn) btn.classList.add('active');
        this.render();
    };

    MapEditor.prototype.paintCell = function(col, row) {
        if (!this.inBounds(col, row)) return;
        var old = this.tiles[row][col];
        var val = this.tool === 'erase' ? 0 : this.selectedBrick;
        if (old === val) return;
        if (!this.currentStroke) this.currentStroke = {};
        var key = row + ',' + col;
        if (!(key in this.currentStroke)) this.currentStroke[key] = old;
        this.tiles[row][col] = val;
    };

    MapEditor.prototype.floodFill = function(col, row) {
        if (!this.inBounds(col, row)) return;
        var target = this.tiles[row][col];
        var fill = this.selectedBrick;
        if (target === fill) return;

        var changes = {};
        var stack = [[col, row]];
        var visited = {};

        while (stack.length > 0) {
            var pos = stack.pop();
            var c = pos[0], r = pos[1];
            var key = r + ',' + c;
            if (visited[key]) continue;
            if (!this.inBounds(c, r)) continue;
            if (this.tiles[r][c] !== target) continue;

            visited[key] = true;
            changes[key] = this.tiles[r][c];
            this.tiles[r][c] = fill;

            stack.push([c + 1, r]);
            stack.push([c - 1, r]);
            stack.push([c, r + 1]);
            stack.push([c, r - 1]);
        }

        this.pushUndo(changes);
    };

    MapEditor.prototype.onMouseDown = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;

        if (e.button === 1 || (this.spaceHeld && e.button === 0)) {
            this.isPanning = true;
            this.lastPanX = e.clientX;
            this.lastPanY = e.clientY;
            return;
        }

        var cell = this.screenToCell(sx, sy);

        if (this.tool === 'paint' || this.tool === 'erase') {
            this.isDrawing = true;
            this.currentStroke = {};
            this.paintCell(cell.col, cell.row);
            this.render();
        } else if (this.tool === 'fill') {
            this.floodFill(cell.col, cell.row);
            this.render();
        } else if (this.tool === 'select') {
            if (this.selection && this.selectionTiles) {
                var s = this.selection;
                var minCol = Math.min(s.startCol, s.endCol);
                var maxCol = Math.max(s.startCol, s.endCol);
                var minRow = Math.min(s.startRow, s.endRow);
                var maxRow = Math.max(s.startRow, s.endRow);
                if (cell.col >= minCol && cell.col <= maxCol && cell.row >= minRow && cell.row <= maxRow) {
                    this.isDraggingSelection = true;
                    this.dragOffsetCol = cell.col - minCol;
                    this.dragOffsetRow = cell.row - minRow;
                    return;
                }
            }
            this.isSelecting = true;
            this.selection = {startCol: cell.col, startRow: cell.row, endCol: cell.col, endRow: cell.row};
            this.selectionTiles = null;
            this.render();
        } else if (this.tool === 'spawn') {
            var world = this.screenToWorld(sx, sy);
            this.spawnPoints.push({x: Math.round(world.x * 10) / 10, y: Math.round(world.y * 10) / 10});
            this.renderSpawnList();
            this.render();
        }
    };

    MapEditor.prototype.onMouseMove = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;

        var cell = this.screenToCell(sx, sy);
        var world = this.screenToWorld(sx, sy);
        document.getElementById('coordsDisplay').textContent =
            'Cell: ' + cell.col + ',' + cell.row + ' | Px: ' + Math.round(world.x) + ',' + Math.round(world.y);

        if (this.isPanning) {
            this.panX += e.clientX - this.lastPanX;
            this.panY += e.clientY - this.lastPanY;
            this.lastPanX = e.clientX;
            this.lastPanY = e.clientY;
            this.render();
            return;
        }

        if (this.isDrawing && (this.tool === 'paint' || this.tool === 'erase')) {
            this.paintCell(cell.col, cell.row);
            this.render();
        }

        if (this.isSelecting && this.tool === 'select') {
            this.selection.endCol = cell.col;
            this.selection.endRow = cell.row;
            this.render();
        }

        if (this.isDraggingSelection && this.selectionTiles) {
            var newMinCol = cell.col - this.dragOffsetCol;
            var newMinRow = cell.row - this.dragOffsetRow;
            var w = this.selectionTiles[0].length;
            var h = this.selectionTiles.length;
            this.selection = {
                startCol: newMinCol, startRow: newMinRow,
                endCol: newMinCol + w - 1, endRow: newMinRow + h - 1
            };
            this.render();
        }
    };

    MapEditor.prototype.onMouseUp = function(e) {
        if (this.isPanning) {
            this.isPanning = false;
            return;
        }

        if (this.isDrawing) {
            this.isDrawing = false;
            this.pushUndo(this.currentStroke);
            this.currentStroke = null;
        }

        if (this.isSelecting) {
            this.isSelecting = false;
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var maxCol = Math.max(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            var maxRow = Math.max(s.startRow, s.endRow);
            this.selectionTiles = [];
            for (var r = minRow; r <= maxRow; r++) {
                var row = [];
                for (var c = minCol; c <= maxCol; c++) {
                    row.push(this.inBounds(c, r) ? this.tiles[r][c] : 0);
                }
                this.selectionTiles.push(row);
            }
            this.selection = {startCol: minCol, startRow: minRow, endCol: maxCol, endRow: maxRow};
        }

        if (this.isDraggingSelection && this.selectionTiles) {
            this.isDraggingSelection = false;
            var changes = {};
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            for (var r = 0; r < this.selectionTiles.length; r++) {
                for (var c = 0; c < this.selectionTiles[r].length; c++) {
                    var tr = minRow + r;
                    var tc = minCol + c;
                    if (this.inBounds(tc, tr)) {
                        var key = tr + ',' + tc;
                        changes[key] = this.tiles[tr][tc];
                        this.tiles[tr][tc] = this.selectionTiles[r][c];
                    }
                }
            }
            this.pushUndo(changes);
            this.selectionTiles = null;
            this.selection = null;
            this.render();
        }
    };

    MapEditor.prototype.onWheel = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var mx = e.clientX - rect.left;
        var my = e.clientY - rect.top;

        var oldZoom = this.zoom;
        var factor = e.deltaY < 0 ? 1.1 : 0.9;
        this.zoom = Math.max(0.1, Math.min(10, this.zoom * factor));

        this.panX = mx - (mx - this.panX) * (this.zoom / oldZoom);
        this.panY = my - (my - this.panY) * (this.zoom / oldZoom);

        this.render();
    };

    MapEditor.prototype.toggleGrid = function(show) {
        this.showGrid = show;
        this.render();
    };

    MapEditor.prototype.renderSpawnList = function() {
        var list = document.getElementById('spawnList');
        var self = this;
        list.innerHTML = '';
        this.spawnPoints.forEach(function(sp, i) {
            var div = document.createElement('div');
            div.className = 'spawn-item';
            div.innerHTML = '<span>#' + (i+1) + ': (' + sp.x + ', ' + sp.y + ')</span>' +
                '<button class="btn btn-danger btn-sm" style="padding:2px 6px;font-size:10px">X</button>';
            div.querySelector('button').onclick = function() {
                self.spawnPoints.splice(i, 1);
                self.renderSpawnList();
                self.render();
            };
            list.appendChild(div);
        });
    };

    MapEditor.prototype.render = function() {
        var ctx = this.ctx;
        var w = this.canvas.width;
        var h = this.canvas.height;
        var cs = this.cellSize;

        ctx.clearRect(0, 0, w, h);
        ctx.save();
        ctx.translate(this.panX, this.panY);
        ctx.scale(this.zoom, this.zoom);

        ctx.fillStyle = '#2a2a4e';
        ctx.fillRect(0, 0, this.gridWidth * cs, this.gridHeight * cs);

        // Tiles with polygon rendering
        for (var row = 0; row < this.gridHeight; row++) {
            for (var col = 0; col < this.gridWidth; col++) {
                var tile = this.tiles[row] ? this.tiles[row][col] : null;
                if (tile > 0) {
                    var border = BRICK_BORDERS[tile];
                    var color = BRICK_COLORS[tile] || '#888';
                    var ox = col * cs;
                    var oy = row * cs;

                    if (border) {
                        drawBrickPolygon(ctx, border, cs, ox, oy, color);
                    } else {
                        ctx.fillStyle = color;
                        ctx.fillRect(ox, oy, cs, cs);
                    }
                }
            }
        }

        // Grid lines
        if (this.showGrid && this.zoom > 0.3) {
            ctx.strokeStyle = 'rgba(255,255,255,0.1)';
            ctx.lineWidth = 0.5 / this.zoom;
            for (var x = 0; x <= this.gridWidth; x++) {
                ctx.beginPath();
                ctx.moveTo(x * cs, 0);
                ctx.lineTo(x * cs, this.gridHeight * cs);
                ctx.stroke();
            }
            for (var y = 0; y <= this.gridHeight; y++) {
                ctx.beginPath();
                ctx.moveTo(0, y * cs);
                ctx.lineTo(this.gridWidth * cs, y * cs);
                ctx.stroke();
            }
        }

        // Selection
        if (this.selection) {
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var maxCol = Math.max(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            var maxRow = Math.max(s.startRow, s.endRow);
            ctx.strokeStyle = '#4a6cf7';
            ctx.lineWidth = 2 / this.zoom;
            ctx.setLineDash([4 / this.zoom, 4 / this.zoom]);
            ctx.strokeRect(minCol * cs, minRow * cs, (maxCol - minCol + 1) * cs, (maxRow - minRow + 1) * cs);
            ctx.setLineDash([]);

            if (this.isDraggingSelection && this.selectionTiles) {
                ctx.globalAlpha = 0.6;
                for (var r = 0; r < this.selectionTiles.length; r++) {
                    for (var c = 0; c < this.selectionTiles[r].length; c++) {
                        var t = this.selectionTiles[r][c];
                        if (t) {
                            var b2 = BRICK_BORDERS[t];
                            var c2 = BRICK_COLORS[t] || '#888';
                            var ox2 = (minCol + c) * cs;
                            var oy2 = (minRow + r) * cs;
                            if (b2) {
                                drawBrickPolygon(ctx, b2, cs, ox2, oy2, c2);
                            } else {
                                ctx.fillStyle = c2;
                                ctx.fillRect(ox2, oy2, cs, cs);
                            }
                        }
                    }
                }
                ctx.globalAlpha = 1.0;
            }
        }

        // Spawn points
        ctx.fillStyle = '#FFD700';
        ctx.strokeStyle = '#000';
        ctx.lineWidth = 1 / this.zoom;
        for (var i = 0; i < this.spawnPoints.length; i++) {
            var sp = this.spawnPoints[i];
            ctx.beginPath();
            ctx.arc(sp.x, sp.y, 6 / this.zoom, 0, Math.PI * 2);
            ctx.fill();
            ctx.stroke();
            ctx.fillStyle = '#000';
            ctx.font = (10 / this.zoom) + 'px sans-serif';
            ctx.fillText('' + (i + 1), sp.x + 8 / this.zoom, sp.y + 4 / this.zoom);
            ctx.fillStyle = '#FFD700';
        }

        // Map border
        ctx.strokeStyle = '#fff';
        ctx.lineWidth = 1 / this.zoom;
        ctx.strokeRect(0, 0, this.gridWidth * cs, this.gridHeight * cs);

        ctx.restore();
    };

    window.editor = new MapEditor('editorCanvas', 'canvasWrap');
})();
