package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var petLineCmd = &cobra.Command{
	Use:   "pet-line",
	Short: "Output a compact single-line pet status for the status bar",
	RunE:  runPetLine,
}

func init() {
	rootCmd.AddCommand(petLineCmd)
}

func runPetLine(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return nil // silently skip if store unavailable
	}
	defer s.Close()

	workDir, err := os.Getwd()
	if err != nil {
		return nil
	}

	pet, err := s.GetPet(workDir)
	if err != nil || pet == nil {
		return nil // no pet yet, print nothing
	}

	offset, direction := loadPetLinePos(s, workDir)
	offset, direction = advancePetLinePos(offset, direction, pet)
	savePetLinePos(s, workDir, offset, direction)

	fmt.Print(renderPetLine(pet, offset))
	return nil
}

// loadPetLinePos reads the stored offset and direction from settings.
func loadPetLinePos(s store.Store, workDir string) (offset, direction int) {
	direction = 1
	val, err := s.GetSetting("pet_line_pos:" + workDir)
	if err != nil || val == "" {
		return 0, 1
	}
	parts := strings.SplitN(val, ",", 2)
	if len(parts) == 2 {
		offset, _ = strconv.Atoi(parts[0])
		direction, _ = strconv.Atoi(parts[1])
	}
	if direction == 0 {
		direction = 1
	}
	return offset, direction
}

func savePetLinePos(s store.Store, workDir string, offset, direction int) {
	_ = s.SetSetting("pet_line_pos:"+workDir, strconv.Itoa(offset)+","+strconv.Itoa(direction))
}

// advancePetLinePos moves the pet one step; step size grows as mood decays.
func advancePetLinePos(offset, direction int, pet *store.PetState) (int, int) {
	avg := (pet.Hunger + pet.Happiness + pet.Energy) / 3
	step := 1
	switch {
	case avg < 20:
		step = 4
	case avg < 40:
		step = 3
	case avg < 60:
		step = 2
	}

	const maxOffset = 9 // 0..9 within the 10-wide track
	offset += direction * step
	if offset >= maxOffset {
		offset = maxOffset
		direction = -1
	}
	if offset <= 0 {
		offset = 0
		direction = 1
	}
	return offset, direction
}

func renderPetLine(pet *store.PetState, offset int) string {
	if !pet.IsAlive {
		return fmt.Sprintf("\033[90m💀 %s R.I.P. | %s tok\033[0m\n", pet.Name, fmtTokens(pet.TotalTokens))
	}

	emoji := petEmoji(pet.Species)
	hunger := miniBar(pet.Hunger, 5)
	mood := miniBar(pet.Happiness, 5)
	moodIcon := petMoodIcon(pet.Hunger, pet.Happiness, pet.Energy)
	track := petTrack(emoji, offset)

	return fmt.Sprintf("\033[35m%s\033[0m \033[90m%s\033[0m \033[90m[%s]\033[0m Food:%s Mood:%s %s \033[90m%s tok\033[0m\n",
		track, pet.Name,
		pet.Species,
		hunger, mood,
		moodIcon,
		fmtTokens(pet.TotalTokens),
	)
}

// petTrack renders the pet moving inside a fixed-width arena, e.g. [🐣       ]
func petTrack(emoji string, offset int) string {
	const trackWidth = 10 // inner spaces
	before := strings.Repeat(" ", offset)
	after := strings.Repeat(" ", trackWidth-offset)
	return "[" + before + emoji + after + "]"
}

func petEmoji(species string) string {
	switch species {
	case "egg":
		return "🥚"
	case "baby":
		return "🐣"
	case "child":
		return "🐱"
	case "teen":
		return "😺"
	case "adult":
		return "🦁"
	case "elder":
		return "👑"
	default:
		return "🐾"
	}
}

func petMoodIcon(hunger, happiness, energy int) string {
	avg := (hunger + happiness + energy) / 3
	switch {
	case avg >= 70:
		return "\033[32m♥\033[0m"
	case avg >= 40:
		return "\033[33m~\033[0m"
	case avg >= 20:
		return "\033[33m/\033[0m"
	default:
		return "\033[31m!\033[0m"
	}
}

func miniBar(value, width int) string {
	filled := value * width / 100
	empty := width - filled
	return strings.Repeat("#", filled) + strings.Repeat("-", empty)
}
