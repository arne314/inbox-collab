package matrix_test

import (
	"slices"
	"testing"

	"github.com/arne314/inbox-collab/internal/matrix"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantCommand string
		wantArg     string
		wantArgs    []string
	}{
		{
			"empty",
			"",
			"",
			"",
			[]string{},
		},
		{
			"simple",
			"!cmd",
			"cmd",
			"",
			[]string{},
		},
		{
			"simple_args",
			"!cmd first second third",
			"cmd",
			"first second third",
			[]string{"first", "second", "third"},
		},
		{
			"whitespaces",
			" !  cmd\nfirst \n  second \rthird",
			"cmd",
			"first \n  second \rthird",
			[]string{"first", "second", "third"},
		},
		{
			"cite",
			"> this is an old message\n!cmd some args",
			"cmd",
			"some args",
			[]string{"some", "args"},
		},
		{
			"cite_command",
			"> !ignore this command\n!cmd some args",
			"cmd",
			"some args",
			[]string{"some", "args"},
		},
		{
			"ignore",
			"ignore !this command",
			"",
			"",
			[]string{},
		},
		{
			"ignore_cite",
			"> !ignore this command\njust a !message",
			"",
			"",
			[]string{},
		},
		{
			"ignore_newline",
			"this is just a\n!message",
			"",
			"",
			[]string{},
		},
		{
			"ignore_cite_newline",
			"> !ignore this command\njust a !message\n!another message",
			"",
			"",
			[]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, arg, args := matrix.ParseCommand(tt.message)
			if command != tt.wantCommand {
				t.Errorf("command ParseCommand() = %v, want %v", command, tt.wantCommand)
			}
			if arg != tt.wantArg {
				t.Errorf("argument ParseCommand() = %v, want %v", arg, tt.wantArg)
			}
			if !slices.Equal(args, tt.wantArgs) {
				t.Errorf("arguments ParseCommand() = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestFormatStateMessage(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			"empty",
			"",
			"",
		},
		{
			"trim",
			" Error. \n ",
			"Error.",
		},
		{
			"dot",
			"This is an error message",
			"This is an error message.",
		},
		{
			"capitalize",
			"message here.",
			"Message here.",
		},
		{
			"full",
			" a message requiring formatting  ",
			"A message requiring formatting.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matrix.FormatStateMessage(tt.s)
			if got != tt.want {
				t.Errorf("FormatStateMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
