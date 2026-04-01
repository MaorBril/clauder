package cmd

import (
	"fmt"
	"os"
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

	fmt.Print(renderPetLine(pet))
	return nil
}

func renderPetLine(pet *store.PetState) string {
	if !pet.IsAlive {
		return fmt.Sprintf("\033[90m💀 %s R.I.P. | %s tok\033[0m\n", pet.Name, fmtTokens(pet.TotalTokens))
	}

	emoji := petEmoji(pet.Species)
	hunger := miniBar(pet.Hunger, 5)
	mood := miniBar(pet.Happiness, 5)

	moodIcon := petMoodIcon(pet.Hunger, pet.Happiness, pet.Energy)

	return fmt.Sprintf("\033[35m%s %s\033[0m \033[90m[%s]\033[0m Food:%s Mood:%s %s \033[90m%s tok\033[0m\n",
		emoji, pet.Name,
		pet.Species,
		hunger, mood,
		moodIcon,
		fmtTokens(pet.TotalTokens),
	)
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
