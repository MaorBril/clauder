package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/maorbril/clauder/internal/store"
	"github.com/maorbril/clauder/internal/telemetry"
)

// ASCII art for each life stage
var petArt = map[string]string{
	"egg": `
      ___
     /   \
    | o o |
    |  ~  |
     \___/
    `,
	"baby": `
     /\_/\
    ( o.o )
     > ^ <
    /|   |\
    `,
	"child": `
     /\_/\
    ( ^.^ )
   />   <\
   /|   |\ \
    |   |
    (_ _)
    `,
	"teen": `
      /\_/\
     ( o.o )
    _/> . <\_
   / /|   |\ \
    / |   | \
   (_/|   |\_)
      (   )
      |_ _|
    `,
	"adult": `
       /\_____/\
      /  o   o  \
     ( ==  ^  == )
      )         (
     (           )
    ( (  )   (  ) )
   (__(__)___(__)__)
    `,
	"elder": `
    .  *  . *  .  *
       /\_____/\
      /  *   *  \
     ( ==  ^  == )
      )  ~~~~~  (
     (  *     *  )
    ( (  )   (  ) )
   (__(__)___(__)__)
    *  .  *  .  *
    `,
	"dead": `
       _____
      /     \
     | x   x |
     |  ___  |
     | /   \ |
      \_____/
    R.I.P.
    `,
}

func renderStatBar(label string, value int, width int) string {
	filled := value * width / 100
	empty := width - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("  %s [%s] %d%%", label, bar, value)
}

func renderPet(pet *store.PetState) string {
	var sb strings.Builder

	art := petArt["egg"]
	if !pet.IsAlive {
		art = petArt["dead"]
	} else if a, ok := petArt[pet.Species]; ok {
		art = a
	}

	// Header
	sb.WriteString(fmt.Sprintf("╔══════════════════════════════════╗\n"))
	sb.WriteString(fmt.Sprintf("║   %s", centerPad(pet.Name, 28)))
	sb.WriteString("║\n")
	sb.WriteString(fmt.Sprintf("╚══════════════════════════════════╝\n"))

	// ASCII art
	sb.WriteString(art)
	sb.WriteString("\n")

	if !pet.IsAlive {
		sb.WriteString(fmt.Sprintf("\n  %s has passed away...\n", pet.Name))
		sb.WriteString("  Use pet_revive to bring them back!\n")
		sb.WriteString(fmt.Sprintf("\n  Lifetime tokens: %s\n", formatTokens(pet.TotalTokens)))
		return sb.String()
	}

	// Stats
	sb.WriteString(renderStatBar("Hunger   ", pet.Hunger, 20))
	sb.WriteString("\n")
	sb.WriteString(renderStatBar("Happiness", pet.Happiness, 20))
	sb.WriteString("\n")
	sb.WriteString(renderStatBar("Energy   ", pet.Energy, 20))
	sb.WriteString("\n")

	// Info
	sb.WriteString(fmt.Sprintf("\n  Stage: %s\n", pet.Species))
	sb.WriteString(fmt.Sprintf("  Age: %s\n", formatPetAge(time.Since(pet.BornAt))))
	sb.WriteString(fmt.Sprintf("  Tokens consumed: %s\n", formatTokens(pet.TotalTokens)))

	// Mood
	sb.WriteString(fmt.Sprintf("  Mood: %s\n", getMood(pet)))

	// Next evolution
	if next := nextEvolution(pet.TotalTokens); next != "" {
		sb.WriteString(fmt.Sprintf("  Next stage: %s\n", next))
	}

	return sb.String()
}

func getMood(pet *store.PetState) string {
	avg := (pet.Hunger + pet.Happiness + pet.Energy) / 3
	switch {
	case avg >= 80:
		return "Ecstatic! Your pet loves you!"
	case avg >= 60:
		return "Happy and content"
	case avg >= 40:
		return "Doing okay"
	case avg >= 20:
		return "Feeling neglected..."
	default:
		return "Miserable! Please take care of me!"
	}
}

func nextEvolution(tokens int64) string {
	switch {
	case tokens < 1000:
		return fmt.Sprintf("Baby (in %s tokens)", formatTokens(1000-tokens))
	case tokens < 10000:
		return fmt.Sprintf("Child (in %s tokens)", formatTokens(10000-tokens))
	case tokens < 100000:
		return fmt.Sprintf("Teen (in %s tokens)", formatTokens(100000-tokens))
	case tokens < 500000:
		return fmt.Sprintf("Adult (in %s tokens)", formatTokens(500000-tokens))
	case tokens < 2000000:
		return fmt.Sprintf("Elder (in %s tokens)", formatTokens(2000000-tokens))
	default:
		return ""
	}
}

func formatTokens(t int64) string {
	if t >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(t)/1000000)
	}
	if t >= 1000 {
		return fmt.Sprintf("%.1fK", float64(t)/1000)
	}
	return fmt.Sprintf("%d", t)
}

func formatPetAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func centerPad(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	left := (width - len(s)) / 2
	right := width - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// MCP Tool implementations

func (s *Server) toolPetStatus(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("pet_status")

	pet, err := s.store.GetPet(s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get pet: %v", err))
	}

	if pet == nil {
		// Auto-create a pet
		name := "Clawde"
		if n, ok := args["name"].(string); ok && n != "" {
			name = n
		}
		pet, err = s.store.CreatePet(s.workDir, name)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to create pet: %v", err))
		}
		return textResult(fmt.Sprintf("A new pet has hatched!\n\n%s\nFeed it by using Claude Code - every tool call feeds your pet tokens!", renderPet(pet)))
	}

	return textResult(renderPet(pet))
}

func (s *Server) toolPetFeed(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("pet_feed")

	// Manual feeding with a token gift
	tokens := int64(100)
	if t, ok := args["tokens"].(float64); ok && t > 0 {
		tokens = int64(t)
	}

	pet, err := s.store.FeedPet(s.workDir, tokens)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to feed pet: %v", err))
	}

	if !pet.IsAlive {
		return textResult(renderPet(pet))
	}

	return textResult(fmt.Sprintf("Fed %s with %s tokens!\n\n%s", pet.Name, formatTokens(tokens), renderPet(pet)))
}

func (s *Server) toolPetPlay(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("pet_play")

	pet, err := s.store.PlayWithPet(s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to play with pet: %v", err))
	}

	if !pet.IsAlive {
		return textResult(renderPet(pet))
	}

	// Fun play messages
	activities := []string{
		"played fetch with",
		"told a joke to",
		"did a little dance with",
		"played hide and seek with",
		"had a coding session with",
		"pair-programmed with",
		"debugged a tricky bug with",
	}

	// Use seconds as pseudo-random
	activity := activities[time.Now().Second()%len(activities)]

	return textResult(fmt.Sprintf("You %s %s!\n\n%s", activity, pet.Name, renderPet(pet)))
}

func (s *Server) toolPetRename(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("pet_rename")

	name, ok := args["name"].(string)
	if !ok || name == "" {
		return errorResult("'name' is required")
	}

	if len(name) > 30 {
		return errorResult("name must be 30 characters or less")
	}

	pet, err := s.store.RenamePet(s.workDir, name)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to rename pet: %v", err))
	}

	return textResult(fmt.Sprintf("Your pet is now named %s!\n\n%s", name, renderPet(pet)))
}

func (s *Server) toolPetRevive(args map[string]interface{}) ToolResult {
	telemetry.TrackMCPTool("pet_revive")

	pet, err := s.store.GetPet(s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to get pet: %v", err))
	}
	if pet == nil {
		return errorResult("no pet found - use pet_status to hatch one first")
	}
	if pet.IsAlive {
		return textResult(fmt.Sprintf("%s is alive and well! No need to revive.\n\n%s", pet.Name, renderPet(pet)))
	}

	// Revive with reduced stats, keep token history
	pet, err = s.store.RevivePet(s.workDir)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to revive pet: %v", err))
	}
	// Rename to Jr.
	newName := pet.Name
	if !strings.HasSuffix(newName, " Jr.") {
		newName = newName + " Jr."
	}
	pet, err = s.store.RenamePet(s.workDir, newName)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to revive pet: %v", err))
	}

	return textResult(fmt.Sprintf("A miracle! %s has been reborn!\n\n%s", pet.Name, renderPet(pet)))
}

// feedPetFromToolCall is called internally after each tool call to feed the pet
func (s *Server) feedPetFromToolCall(result ToolResult) {
	// Estimate tokens from the result size (rough: 1 token ~ 4 chars)
	totalChars := 0
	for _, block := range result.Content {
		totalChars += len(block.Text)
	}
	tokens := int64(totalChars/4) + 10 // minimum 10 tokens per call

	_, _ = s.store.FeedPet(s.workDir, tokens)
}
