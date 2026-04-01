package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/spf13/cobra"
)

var petCmd = &cobra.Command{
	Use:   "pet",
	Short: "Check on your Tamagotchi pet",
	Long:  "View your Tamagotchi pet's status. Your pet feeds on tokens consumed during Claude Code sessions.",
	RunE:  runPet,
}

func init() {
	rootCmd.AddCommand(petCmd)
}

func runPet(cmd *cobra.Command, args []string) error {
	dataDir := getDataDir()
	s, err := store.NewSQLiteStore(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer s.Close()

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	pet, err := s.GetPet(workDir)
	if err != nil {
		return fmt.Errorf("failed to get pet: %w", err)
	}

	if pet == nil {
		fmt.Println("No pet found for this directory.")
		fmt.Println("Start using Claude Code tools to hatch your pet, or call the pet_status MCP tool!")
		return nil
	}

	fmt.Println(renderPetCLI(pet))
	return nil
}

func renderPetCLI(pet *store.PetState) string {
	var sb strings.Builder

	art := store.PetArt(pet.Species, pet.IsAlive)
	sb.WriteString(art)
	sb.WriteString("\n")

	if !pet.IsAlive {
		sb.WriteString(fmt.Sprintf("  %s has passed away...\n", pet.Name))
		sb.WriteString(fmt.Sprintf("  Lifetime tokens: %s\n", fmtTokens(pet.TotalTokens)))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("  Name:      %s\n", pet.Name))
	sb.WriteString(fmt.Sprintf("  Stage:     %s\n", pet.Species))
	sb.WriteString(fmt.Sprintf("  Hunger:    %s %d%%\n", statBar(pet.Hunger, 20), pet.Hunger))
	sb.WriteString(fmt.Sprintf("  Happiness: %s %d%%\n", statBar(pet.Happiness, 20), pet.Happiness))
	sb.WriteString(fmt.Sprintf("  Energy:    %s %d%%\n", statBar(pet.Energy, 20), pet.Energy))
	sb.WriteString(fmt.Sprintf("  Age:       %s\n", petAge(time.Since(pet.BornAt))))
	sb.WriteString(fmt.Sprintf("  Tokens:    %s\n", fmtTokens(pet.TotalTokens)))

	return sb.String()
}

func statBar(value int, width int) string {
	filled := value * width / 100
	empty := width - filled
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", empty) + "]"
}

func miniBar(value, width int) string {
	filled := value * width / 100
	empty := width - filled
	return strings.Repeat("#", filled) + strings.Repeat("-", empty)
}

func fmtTokens(t int64) string {
	if t >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(t)/1000000)
	}
	if t >= 1000 {
		return fmt.Sprintf("%.1fK", float64(t)/1000)
	}
	return fmt.Sprintf("%d", t)
}

func petAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
}

