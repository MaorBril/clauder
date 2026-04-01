//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"golang.org/x/term"
)

const petOverlayLines = 2 // number of terminal lines reserved for the pet

// petOverlay renders a persistent pet status bar at the bottom of the terminal.
// It uses ANSI escape codes to draw outside the scroll region so it doesn't
// interfere with Claude Code's output.
type petOverlay struct {
	store   store.Store
	workDir string
	stopCh  chan struct{}
	mu      sync.Mutex

	// animation state
	petOffset int  // horizontal position for wandering
	direction int  // 1 = right, -1 = left
	frame     int  // animation frame counter
	lastMood  int  // cached mood for movement decisions
	termWidth int  // cached terminal width
}

func newPetOverlay(s store.Store, workDir string) *petOverlay {
	w, _, _ := term.GetSize(int(os.Stdout.Fd()))
	if w <= 0 {
		w = 80
	}
	return &petOverlay{
		store:     s,
		workDir:   workDir,
		stopCh:    make(chan struct{}),
		petOffset: w / 2,
		direction: 1,
		termWidth: w,
	}
}

// Start begins the overlay rendering loop
func (p *petOverlay) Start() {
	p.setupScrollRegion()
	go p.run()
}

// Stop cleans up the overlay
func (p *petOverlay) Stop() {
	close(p.stopCh)
	p.mu.Lock()
	defer p.mu.Unlock()

	_, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if h > 0 {
		// Reset scroll region to full terminal
		fmt.Fprintf(os.Stdout, "\033[1;%dr", h)
		// Clear the pet lines
		fmt.Fprintf(os.Stdout, "\033[s")
		fmt.Fprintf(os.Stdout, "\033[%d;1H\033[K", h-1)
		fmt.Fprintf(os.Stdout, "\033[%d;1H\033[K", h)
		fmt.Fprintf(os.Stdout, "\033[u")
	} else {
		fmt.Fprintf(os.Stdout, "\033[r") // reset to default
	}
}

// HandleResize should be called on SIGWINCH
func (p *petOverlay) HandleResize() {
	p.mu.Lock()
	w, _, _ := term.GetSize(int(os.Stdout.Fd()))
	if w > 0 {
		p.termWidth = w
	}
	p.mu.Unlock()
	p.setupScrollRegion()
	p.render()
}

func (p *petOverlay) setupScrollRegion() {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if h <= petOverlayLines+1 {
		return // terminal too small
	}
	// Reserve bottom lines: set scroll region to rows 1..(h-petOverlayLines)
	fmt.Fprintf(os.Stdout, "\033[1;%dr", h-petOverlayLines)
}

func (p *petOverlay) run() {
	// Small delay to let Claude Code initialize first
	select {
	case <-p.stopCh:
		return
	case <-time.After(2 * time.Second):
	}

	p.render()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.updateAnimation()
			p.render()
		}
	}
}

func (p *petOverlay) updateAnimation() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.frame++

	// Move when pet is bored (low happiness)
	if p.lastMood < 40 {
		step := 2
		if p.lastMood < 20 {
			step = 3 // more frantic when really unhappy
		}
		p.petOffset += p.direction * step
		maxOffset := max(p.termWidth-20, 10)
		if p.petOffset >= maxOffset {
			p.direction = -1
		}
		if p.petOffset <= 2 {
			p.direction = 1
		}
	}
}

func (p *petOverlay) render() {
	pet, err := p.store.GetPet(p.workDir)
	if err != nil || pet == nil {
		return // no pet yet
	}

	p.mu.Lock()
	avg := (pet.Hunger + pet.Happiness + pet.Energy) / 3
	p.lastMood = avg
	w := p.termWidth
	offset := p.petOffset
	frame := p.frame
	p.mu.Unlock()

	_, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if h <= petOverlayLines+1 {
		return
	}

	// Build the two display lines
	line1, line2 := p.buildPetDisplay(pet, offset, w, frame)

	// Render in the reserved bottom area using ANSI escape codes
	var sb strings.Builder
	sb.WriteString("\033[s")                              // save cursor
	sb.WriteString("\033[?7l")                            // disable auto-wrap to prevent scroll on last line
	sb.WriteString(fmt.Sprintf("\033[%d;1H", h-1))        // goto line h-1
	sb.WriteString("\033[K")                              // clear line
	sb.WriteString(line1)
	sb.WriteString(fmt.Sprintf("\033[%d;1H", h))           // goto line h
	sb.WriteString("\033[K")                              // clear line
	sb.WriteString(line2)
	sb.WriteString("\033[?7h")                            // re-enable auto-wrap
	sb.WriteString("\033[u")                              // restore cursor

	os.Stdout.WriteString(sb.String())
}

func (p *petOverlay) buildPetDisplay(pet *store.PetState, offset, width, frame int) (string, string) {
	if !pet.IsAlive {
		sprite := "(x_x)"
		return padSprite(sprite, offset, width), buildOverlayStats(pet, width)
	}

	sprite := overlaySprite(pet.Species, frame)
	return padSprite(sprite, offset, width), buildOverlayStats(pet, width)
}

func overlaySprite(species string, frame int) string {
	even := frame%2 == 0

	switch species {
	case "egg":
		if even {
			return "(o.o)"
		}
		return "(o,o)"
	case "baby":
		if even {
			return "/\\_/\\(=^.^=)"
		}
		return "/\\_/\\(=^,^=)"
	case "child":
		if even {
			return "~(=^.^=)~"
		}
		return "~(=^o^=)~"
	case "teen":
		if even {
			return "\\(=^.^=)/ ~"
		}
		return "~ \\(=^.^=)/"
	case "adult":
		if even {
			return "=^..^=  {\\_/}"
		}
		return "=^..^= {\\_ /}"
	case "elder":
		if even {
			return "*.=^..^=.* {\\__/}"
		}
		return ".*=^..^=*. {\\__/}"
	default:
		return "(?_?)"
	}
}

func buildOverlayStats(pet *store.PetState, width int) string {
	if !pet.IsAlive {
		return overlayCenter(fmt.Sprintf(" R.I.P. %s | Tokens: %s ", pet.Name, overlayFmtTokens(pet.TotalTokens)), width)
	}

	hungerBar := overlayMiniBar(pet.Hunger)
	happyBar := overlayMiniBar(pet.Happiness)

	mood := ""
	avg := (pet.Hunger + pet.Happiness + pet.Energy) / 3
	switch {
	case avg >= 70:
		mood = "<3"
	case avg >= 40:
		mood = ":)"
	case avg >= 20:
		mood = ":/"
	default:
		mood = ":("
	}

	line := fmt.Sprintf(" %s [%s] | Food:%s Mood:%s | %s | %s ",
		pet.Name, pet.Species,
		hungerBar, happyBar,
		mood,
		overlayFmtTokens(pet.TotalTokens))

	return overlayCenter(line, width)
}

func overlayMiniBar(value int) string {
	filled := value / 20 // 0-5 blocks
	empty := 5 - filled
	return strings.Repeat("#", filled) + strings.Repeat("-", empty)
}

func padSprite(sprite string, offset, width int) string {
	offset = max(offset, 0)
	if offset+len(sprite) > width {
		offset = max(width-len(sprite), 0)
	}
	return strings.Repeat(" ", offset) + sprite
}

func overlayCenter(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	pad := (width - len(s)) / 2
	return strings.Repeat(" ", pad) + s
}

func overlayFmtTokens(t int64) string {
	return fmtTokens(t) + " tok"
}
