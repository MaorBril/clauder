//go:build !windows

package cmd

import (
	"testing"
)

func TestParseWrapArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFlags  wrapFlags
		wantClaude []string
	}{
		{
			name:       "no args",
			args:       []string{},
			wantFlags:  wrapFlags{},
			wantClaude: nil,
		},
		{
			name:       "only claude args (backwards compat)",
			args:       []string{"-p", "fix the bug"},
			wantFlags:  wrapFlags{},
			wantClaude: []string{"-p", "fix the bug"},
		},
		{
			name:       "double dash separator with claude args",
			args:       []string{"--", "-p", "fix the bug"},
			wantFlags:  wrapFlags{},
			wantClaude: []string{"-p", "fix the bug"},
		},
		{
			name:       "name flag with double dash",
			args:       []string{"--name", "backend", "--", "--resume"},
			wantFlags:  wrapFlags{name: "backend"},
			wantClaude: []string{"--resume"},
		},
		{
			name:       "short name flag with double dash",
			args:       []string{"-n", "frontend", "--", "-p", "hello"},
			wantFlags:  wrapFlags{name: "frontend"},
			wantClaude: []string{"-p", "hello"},
		},
		{
			name:       "name flag without double dash",
			args:       []string{"--name", "backend"},
			wantFlags:  wrapFlags{name: "backend"},
			wantClaude: nil,
		},
		{
			name:       "name flag mixed with claude args no separator",
			args:       []string{"--name", "backend", "--resume"},
			wantFlags:  wrapFlags{name: "backend"},
			wantClaude: []string{"--resume"},
		},
		{
			name:       "help flag",
			args:       []string{"--help"},
			wantFlags:  wrapFlags{help: true},
			wantClaude: nil,
		},
		{
			name:       "help short flag",
			args:       []string{"-h"},
			wantFlags:  wrapFlags{help: true},
			wantClaude: nil,
		},
		{
			name:       "double dash only",
			args:       []string{"--"},
			wantFlags:  wrapFlags{},
			wantClaude: nil,
		},
		{
			name:       "name and help before double dash",
			args:       []string{"--name", "test", "-h", "--"},
			wantFlags:  wrapFlags{name: "test", help: true},
			wantClaude: nil,
		},
		{
			name:       "resume session with double dash",
			args:       []string{"--", "--resume", "1335765446"},
			wantFlags:  wrapFlags{},
			wantClaude: []string{"--resume", "1335765446"},
		},
		{
			name:       "telegram flag with double dash",
			args:       []string{"--telegram", "--", "-p", "hello"},
			wantFlags:  wrapFlags{telegram: true},
			wantClaude: []string{"-p", "hello"},
		},
		{
			name:       "telegram flag without double dash",
			args:       []string{"--telegram"},
			wantFlags:  wrapFlags{telegram: true},
			wantClaude: nil,
		},
		{
			name:       "telegram with name and claude args",
			args:       []string{"--name", "bot", "--telegram", "--", "--resume"},
			wantFlags:  wrapFlags{name: "bot", telegram: true},
			wantClaude: []string{"--resume"},
		},
		{
			name:       "telegram without separator mixed with claude args",
			args:       []string{"--telegram", "--name", "bot", "--resume"},
			wantFlags:  wrapFlags{name: "bot", telegram: true},
			wantClaude: []string{"--resume"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFlags, gotClaude := parseWrapArgs(tt.args)

			if gotFlags.name != tt.wantFlags.name {
				t.Errorf("name = %q, want %q", gotFlags.name, tt.wantFlags.name)
			}

			if gotFlags.help != tt.wantFlags.help {
				t.Errorf("help = %v, want %v", gotFlags.help, tt.wantFlags.help)
			}

			if gotFlags.telegram != tt.wantFlags.telegram {
				t.Errorf("telegram = %v, want %v", gotFlags.telegram, tt.wantFlags.telegram)
			}

			if len(gotClaude) != len(tt.wantClaude) {
				t.Errorf("claudeArgs = %v, want %v", gotClaude, tt.wantClaude)
				return
			}
			for i := range gotClaude {
				if gotClaude[i] != tt.wantClaude[i] {
					t.Errorf("claudeArgs[%d] = %q, want %q", i, gotClaude[i], tt.wantClaude[i])
				}
			}
		})
	}
}
