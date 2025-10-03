package textprocessor

import (
	"testing"
)

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

func Test_smithWaterman(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		substring string
		match     string
		minScore  float32
	}{
		// trivial cases
		{
			"empty",
			"",
			"empty",
			"",
			0,
		},
		{
			"full",
			"full match",
			"full match",
			"full match",
			1,
		},
		{
			"none",
			"no match here",
			"this is just different",
			"",
			0,
		},
		{
			"none",
			"almost no match here",
			"this is just different here",
			"here",
			0.1,
		},
		{
			"caps",
			"mAtcH",
			"Match",
			"mAtcH",
			1,
		},
		// start
		{
			"start",
			"another simple match",
			"another simple",
			"another simple",
			1,
		},
		// middle
		{
			"middle",
			"hello lol hi lol",
			"lol hi",
			"lol hi",
			1,
		},
		// end
		{
			"end",
			"a simple match",
			"simple match",
			"simple match",
			1,
		},
		// simple incomplete match
		{
			"incomplete",
			"this string almost contains my little secret string and something else",
			"my secret string",
			"my little secret string",
			0.8,
		},
		{
			"incomplete",
			"this is easy 1+1=2",
			"this is ez 1+1=2",
			"this is easy 1+1=2",
			0.5,
		},
		// incomplete match with additional special characters
		{
			"special",
			":This is valid",
			"this is valid",
			":This is valid",
			0.9,
		},
		{
			"special",
			"we have some additional - special >> characters in here",
			"additional special characters in here",
			"additional - special >> characters in here",
			0.8,
		},
		{
			"special",
			"hi this is a rather long string where we are going to find this little(!) part here and not this little part there",
			"this little part here",
			"this little(!) part here",
			0.8,
		},
		// incomplete match with different word
		{
			"word",
			":This is not valid ...",
			"haha is not valid",
			"is not valid",
			0.7,
		},
		// we prefer a gap in the block over a gap in the base
		{
			"gap_order",
			"1 2 3 A B B C D E",
			"A B C",
			"A B B C", // instead of just "A B" or "B C"
			0.8,
		},
		{
			"gap_order",
			"1 2 3 A B B C C D E",
			"A B C D",
			"A B B C C D", // instead of shorter substrings
			0.7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			similarity, start, end := smithWaterman(computeMessageChunks(&tt.content), computeMessageChunks(&tt.substring))
			got := tt.content[start:end]
			if got != tt.match {
				t.Errorf("smithWaterman() = %v, want %v", got, tt.match)
			}
			if similarity < tt.minScore {
				t.Errorf("smithWaterman() similarity not sufficient = %v, want %v for %v", similarity, tt.minScore, tt.match)
			}
		})
	}
}
