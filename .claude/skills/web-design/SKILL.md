# Web Design Skill - Clauder Dashboard

Use this skill when designing, improving, or modifying the Clauder web management interface.

## Architecture Overview

The web dashboard is a **single-page application** served by a Go HTTP server. All UI lives in one file:

- **Template**: `internal/ui/templates/dashboard.html` (~2,570 lines)
  - Inline `<style>` block (CSS custom properties + pure CSS)
  - Semantic HTML structure
  - Vanilla JavaScript (no frameworks)
- **Server**: `internal/ui/web.go` (~665 lines)
  - Go `html/template` rendering
  - REST API endpoints
  - WebSocket terminal support
- **Embedded**: Templates are embedded in the binary via `//go:embed templates/*`

## Technology Stack

| Layer | Technology |
|-------|-----------|
| HTML | HTML5 with Go template directives (`{{.RefreshInterval}}`, `{{.WorkDir}}`) |
| CSS | Pure CSS with CSS custom properties, Flexbox/Grid, no preprocessor |
| JavaScript | Vanilla JS, no build step, no frameworks |
| Fonts | Google Fonts: Inter (UI), JetBrains Mono (code/monospace) |
| Terminal | XTerm.js v5.3.0 + fit addon + web-links addon |
| Real-time | WebSockets (Gorilla) for terminal PTY |
| Icons | Unicode/emoji characters (no icon library) |

## Design System

### Color Palette (CSS Variables)

```css
:root {
    /* Primary - Purple */
    --primary: #8B5CF6;
    --primary-light: #A78BFA;
    --primary-dark: #7C3AED;

    /* Secondary - Cyan */
    --secondary: #06B6D4;
    --secondary-light: #22D3EE;

    /* Semantic */
    --success: #10B981;
    --success-light: #34D399;
    --warning: #F59E0B;
    --warning-light: #FBBF24;
    --error: #EF4444;

    /* Neutral */
    --muted: #64748B;
    --muted-light: #94A3B8;
    --text: #F1F5F9;
    --text-secondary: #CBD5E1;

    /* Backgrounds - Dark theme */
    --bg: #0F172A;           /* Page background */
    --bg-card: #1E293B;      /* Card/surface background */
    --bg-card-hover: #334155; /* Hover state */

    /* Borders */
    --border: #334155;
    --border-light: #475569;

    /* Glass effects */
    --glass: rgba(30, 41, 59, 0.8);
    --glass-border: rgba(148, 163, 184, 0.1);

    /* Shadows */
    --shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.3), 0 2px 4px -2px rgba(0, 0, 0, 0.2);
    --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.3), 0 4px 6px -4px rgba(0, 0, 0, 0.2);

    /* Border radius */
    --radius: 12px;
    --radius-sm: 8px;
    --radius-lg: 16px;
}
```

### Typography

- **UI text**: `'Inter', -apple-system, BlinkMacSystemFont, sans-serif`
- **Code/IDs**: `'JetBrains Mono', monospace`
- **Base line-height**: 1.5
- **Font sizes**: 0.6875rem (tags) → 1.75rem (page titles)
- **Font weights**: 300 (light) through 700 (bold)

### Spacing Conventions

- Padding: 0.25rem increments (0.25, 0.375, 0.5, 0.625, 0.75, 0.875, 1, 1.25, 1.5, 2rem)
- Gap: 0.375rem (tight) → 1.5rem (sections)
- Sidebar width: 280px (240px on tablet, hidden on mobile)

### Component Patterns

**Cards**: `background: var(--bg-card)`, `border: 1px solid var(--border)`, `border-radius: var(--radius)`, hover lifts with `var(--shadow)`

**Badges**: Pill shape (`border-radius: 9999px`), semi-transparent bg with matching text color:
- Leader/Active: `rgba(16, 185, 129, 0.15)` + `var(--success-light)`
- Idle/Warning: `rgba(245, 158, 11, 0.15)` + `var(--warning-light)`
- Inactive: `rgba(100, 116, 139, 0.15)` + `var(--muted-light)`

**Tags**: `border-radius: var(--radius-sm)`, `background: rgba(6, 182, 212, 0.15)`, `color: var(--secondary-light)`

**Buttons**: Two variants:
- `.btn-primary`: Solid `var(--primary)` background, white text
- `.btn-secondary`: `var(--bg-card)` background, `var(--border)` border

**Modals**: Overlay with `backdrop-filter: blur(4px)`, centered card, max-width 640px

**Gradients**: Logo/avatars use `linear-gradient(135deg, var(--primary) 0%, var(--secondary) 100%)`

## Page Structure

### Layout
```
┌─────────────────────────────────────────────┐
│ .app (display: flex)                        │
│ ┌──────────┐ ┌────────────────────────────┐ │
│ │ .sidebar  │ │ .main                      │ │
│ │ (fixed    │ │ (margin-left: 280px)       │ │
│ │  280px)   │ │                            │ │
│ │           │ │ ┌────────────────────────┐ │ │
│ │ - Logo    │ │ │ .view (one visible)    │ │ │
│ │ - Nav     │ │ │  - Map view            │ │ │
│ │ - Stats   │ │ │  - Instances view      │ │ │
│ │           │ │ │  - Messages view       │ │ │
│ │           │ │ │  - Facts view          │ │ │
│ │           │ │ └────────────────────────┘ │ │
│ └──────────┘ └────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

### Views (4 tabs)

1. **Map View** (`#map-view`): Hexagonal grid of instances + session panel sidebar
   - Hex grid uses CSS `clip-path: polygon(...)` for hexagon shape
   - Session list panel (380px wide) on the right
   - Diamond-shaped layout algorithm for hex arrangement

2. **Instances View** (`#instances-view`): Card list with search + filter (All/Active/Idle/Leader)

3. **Messages View** (`#messages-view`): Card list with search + filter (All/Unread/Read) + from/to dropdowns

4. **Facts View** (`#facts-view`): Card list with search + filter (All/Local/Global) + tag/source dropdowns

### Overlays

- **Detail Modal** (`#modal`): Shows instance/message/fact details
- **Launch Modal** (`#launch-modal`): New session launcher with quick-launch list
- **Terminal Overlay** (`#terminal-overlay`): Full-screen XTerm.js terminal
- **Terminals Tray** (`#terminals-tray`): Minimized terminal pills (bottom-right)

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Serve dashboard HTML |
| GET | `/api/data` | JSON: instances, messages, facts, stats |
| POST | `/api/launch` | Launch Terminal.app with claude (macOS) |
| POST | `/api/facts/delete?id=N` | Soft-delete a fact |
| WS | `/api/terminal?dir=PATH` | WebSocket PTY for embedded terminal |

### API Response Shape

```json
{
  "instances": [{ "id", "shortId", "directory", "shortDir", "projectName", "isLeader", "isIdle", "isActive", "needsAttention", "attentionReason", "unreadCount", "lastHeartbeat", "heartbeatAge", "recentMessages" }],
  "messages": [{ "id", "fromInstance", "shortFrom", "fromDir", "shortFromDir", "toInstance", "shortTo", "toDir", "shortToDir", "content", "preview", "createdAt", "age", "isRead" }],
  "facts": [{ "id", "content", "preview", "tags", "sourceDir", "shortDir", "isLocal", "createdAt" }],
  "stats": { "totalInstances", "workingInstances", "totalMessages", "unreadMessages", "totalFacts", "localFacts" }
}
```

## JavaScript Architecture

### State Management
```js
let currentView = 'map';           // Active tab
let allData = { instances: [], messages: [], facts: [], stats: {} };  // API data
let instanceFilter = 'all';        // Instance tab filter
let messageFilter = 'all';         // Message tab filter
let factFilter = 'all';            // Fact tab filter
let selectedInstanceIndex = -1;    // Selected hex/session
let terminals = [];                // Active terminal sessions (max 4)
let activeTerminalId = null;       // Currently displayed terminal
```

### Data Flow
1. `fetchData()` → GET `/api/data` → store in `allData`
2. `updateStats()` → update sidebar badges and stat cards
3. `renderCurrentView()` → dispatches to `renderMap()`, `renderInstances()`, `renderMessages()`, or `renderFacts()`
4. Auto-refresh via `setInterval(fetchData, REFRESH_INTERVAL)`

### Key Functions
- `switchView(view)` - Tab navigation
- `fetchData()` - API fetch + render
- `renderMap()` / `renderInstances()` / `renderMessages()` / `renderFacts()` - View renderers
- `showModal(title, content)` / `closeModal()` - Modal management
- `openTerminal(dir)` / `closeTerminal()` / `minimizeTerminal()` / `restoreTerminal()` - Terminal lifecycle
- `escapeHtml(text)` - XSS prevention via DOM textContent
- `deleteFact(id, btn)` - Fact deletion with optimistic UI

### Keyboard Shortcuts
- `R` - Refresh data
- `Escape` - Close/minimize modals and terminals
- `1-9` - Select instance in map view
- `Alt+1/m` - Map view
- `Alt+2/i` - Instances view
- `Alt+3/e` - Messages view
- `Alt+4/f` - Facts view

## Design Guidelines

When modifying the UI:

1. **Dark theme only** - All colors use the CSS variable system, maintain contrast ratios
2. **No external dependencies** - Keep it vanilla JS/CSS. Only allowed externals: Google Fonts, XTerm.js
3. **Single file** - All HTML/CSS/JS stays in `dashboard.html` (embedded in Go binary)
4. **Go template safety** - Use `{{.Field}}` for server data. HTML is escaped by default
5. **XSS prevention** - Always use `escapeHtml()` for user-generated content in innerHTML
6. **Responsive** - Three breakpoints: desktop (>1024px), tablet (768-1024px), mobile (<768px)
7. **Animations** - Subtle transitions (0.2s ease), pulse animations for attention states
8. **Consistent spacing** - Use existing padding/gap patterns, avoid arbitrary values
9. **Card pattern** - New content types should follow the existing card structure
10. **Filter pattern** - New views should include search box + filter buttons + optional dropdowns

## Adding New Features

### Adding a new view/tab:
1. Add nav item in `.sidebar > .nav` with `data-view="newview"`
2. Add `<div class="view" id="newview-view" style="display: none;">` in `.main`
3. Add case to `renderCurrentView()` switch
4. Add keyboard shortcut in keydown handler
5. Create `renderNewview()` function following existing patterns

### Adding a new API endpoint:
1. Add route in `web.go` `Start()` method
2. Create handler function on `WebServer`
3. Update `APIResponse` struct if extending `/api/data`
4. Update JavaScript `fetchData()` if data shape changes

### Adding new card types:
1. Follow `.fact-card` / `.message-card` / `.instance-card` patterns
2. Include hover state with `var(--shadow)` lift
3. Add click handler for modal details
4. Include empty state with `.empty-state` pattern

## Terminal Integration

The embedded terminal uses:
- XTerm.js for rendering
- WebSocket for bidirectional PTY communication
- FitAddon for responsive sizing
- WebLinksAddon for clickable URLs
- Up to 4 concurrent terminals with minimize/restore
- Terminal theme matches the dashboard dark theme

## Testing Changes

After modifying `dashboard.html`:
1. Run `make build` or `go build -o clauder .`
2. Run `./clauder ui` to start the web server
3. Dashboard opens at `http://localhost:8765`
4. Use `--port` flag for custom port, `--no-browser` to skip auto-open
