package textprocessor

import (
	"testing"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		text       string
		want       string
		allowPunct bool
	}{
		{
			text: "caPitAl LETTERS",
			want: "capital letters",
		},
		{
			text: "too  many   spaces\t\r\nin\nhere",
			want: "too many spaces in here",
		},
		{
			text: " trim spaces   ",
			want: "trim spaces",
		},
		{
			text: "höhÖ hÓhó ﬀ²",
			want: "hoho hoho ff2",
		},
		{
			text:       "!punctuation is \"bad\"!",
			want:       "punctuation is bad",
			allowPunct: false,
		},
		{
			text:       "!punctuation is \"valid\" now!",
			want:       "!punctuation is \"valid\" now!",
			allowPunct: true,
		},
		{
			text: "bye 👀👀 emojis",
			want: "bye emojis",
		},
		{
			text:       "123digits are fine 3.14",
			want:       "123digits are fine 314",
			allowPunct: false,
		},
		{
			text:       "123numbers are fine 3.14",
			want:       "123numbers are fine 3.14",
			allowPunct: true,
		},
	}

	for _, tt := range tests {
		t.Run("Normalization", func(t *testing.T) {
			got := NormalizeText(tt.text, tt.allowPunct)
			if got != tt.want {
				t.Errorf("NormalizeText() = \"%v\", want \"%v\"", got, tt.want)
			}
		})
	}
}

func Test_computeMessageChunks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			"simple",
			"a simple chunk",
			[]string{"a", "simple", "chunk"},
		},
		{
			"lines",
			"a\nfew\nlines",
			[]string{"a", "few", "lines"},
		},
		{
			"spaces",
			"    additional    spaces   don't \tcount\nover\r\rhere\n",
			[]string{"additional", "spaces", "don't", "count", "over", "here"},
		},
		{
			"single",
			"s i n g l e",
			[]string{"s", "i", "n", "g", "l", "e"},
		},
		{
			"special",
			"sp3cíal chäräctérs :) >> (-; 1+1",
			[]string{"sp3cíal", "chäräctérs", ":)", ">>", "(-;", "1+1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeMessageChunks(&tt.content)
			if len(got.chunks) != len(tt.want) {
				t.Errorf("computeMessageChunks() has incorrect length %v, want %v", len(got.chunks), len(tt.want))
				return
			}
			for i, chunk := range got.chunks {
				st := chunkToString(got, chunk)
				if st != tt.want[i] {
					t.Errorf("computeMessageChunks()[%v] = %v, want %v", i, st, tt.want[i])
				}
			}
		})
	}
}
