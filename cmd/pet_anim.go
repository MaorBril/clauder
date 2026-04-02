//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var petAnimCmd = &cobra.Command{
	Use:    "pet-anim",
	Short:  "Show a play animation for the pet",
	Hidden: true,
	RunE:   runPetAnim,
}

func init() {
	rootCmd.AddCommand(petAnimCmd)
}

func runPetAnim(cmd *cobra.Command, args []string) error {
	// Open /dev/tty directly — this command may be spawned from an MCP subprocess
	// whose stdout is captured by JSON-RPC, so we must bypass it.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer tty.Close()

	if !term.IsTerminal(int(tty.Fd())) {
		return nil
	}

	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return nil
	}
	defer s.Close()

	workDir, err := os.Getwd()
	if err != nil {
		return nil
	}

	pet, err := s.GetPet(workDir)
	if err != nil || pet == nil || !pet.IsAlive {
		return nil
	}

	w, h, err := term.GetSize(int(tty.Fd()))
	if err != nil || h < 5 {
		return nil
	}

	emoji := petEmoji(pet.Species)
	frames := buildAnimFrames(emoji, pet.Name, w)

	for i, frame := range frames {
		drawAnimFrame(tty, frame, h)
		if i < len(frames)-1 {
			time.Sleep(320 * time.Millisecond)
		}
	}
	time.Sleep(500 * time.Millisecond)
	clearAnimFrame(tty, h)
	return nil
}

const animHeight = 3 // lines reserved for animation

// buildAnimFrames returns 4 frames of a ball-chase animation.
func buildAnimFrames(emoji, name string, termWidth int) [][]string {
	boxWidth := min(termWidth-4, 44)
	inner := boxWidth - 2 // inside the border chars

	title := centerInWidth("✨ "+name+" is playing! ✨", inner)
	top := "╔" + strings.Repeat("═", inner) + "╗"
	bot := "╚" + strings.Repeat("═", inner) + "╝"

	// Ball positions within inner width (accounting for 2-col emoji)
	positions := []int{2, inner/3 + 1, 2*inner/3 - 1, inner - 4}

	frames := make([][]string, len(positions))
	for i, ballPos := range positions {
		content := buildTrackLine(emoji, "🎾", ballPos, inner)
		frames[i] = []string{
			top,
			"║" + title + "║",
			"║" + content + "║",
			bot,
		}
	}
	// Last frame: caught it
	caught := centerInWidth(emoji+"✨🎾✨", inner)
	frames[len(frames)-1] = []string{
		top,
		"║" + title + "║",
		"║" + caught + "║",
		bot,
	}
	return frames
}

// buildTrackLine places the pet emoji at the right edge and the ball at ballPos.
func buildTrackLine(petEmoji, ball string, ballPos, width int) string {
	left := max(ballPos, 0)
	mid := max(width-left-8, 0) // 8 = 2×emoji display width (approx)
	return fmt.Sprintf("%s%s%s%s%s",
		strings.Repeat(" ", left),
		ball,
		strings.Repeat(" ", mid),
		petEmoji,
		strings.Repeat(" ", max(width-left-mid-4-4, 0)),
	)
}

func drawAnimFrame(tty *os.File, lines []string, termHeight int) {
	var sb strings.Builder
	sb.WriteString("\033[s")    // save cursor
	sb.WriteString("\033[?7l") // disable auto-wrap
	for i, line := range lines {
		row := termHeight - animHeight + i
		sb.WriteString(fmt.Sprintf("\033[%d;1H\033[K", row))
		sb.WriteString(line)
	}
	sb.WriteString("\033[?7h") // re-enable auto-wrap
	sb.WriteString("\033[u")   // restore cursor
	tty.WriteString(sb.String())
}

func clearAnimFrame(tty *os.File, termHeight int) {
	var sb strings.Builder
	sb.WriteString("\033[s")
	for i := range animHeight {
		row := termHeight - animHeight + i + 1
		sb.WriteString(fmt.Sprintf("\033[%d;1H\033[K", row))
	}
	sb.WriteString("\033[u")
	tty.WriteString(sb.String())
}

func centerInWidth(s string, width int) string {
	// Use rune count as approximation (emoji count as 1 rune but 2 cols — close enough)
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	pad := (width - len(runes)) / 2
	right := width - len(runes) - pad
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", right)
}
