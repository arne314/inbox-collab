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
