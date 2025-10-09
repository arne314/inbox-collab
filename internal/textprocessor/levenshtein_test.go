package textprocessor

import "testing"

func Test_levenshtein(t *testing.T) {
	tests := []struct {
		name    string
		s1      string
		s2      string
		wantMin float32
		wantMax float32
	}{
		{
			"empty",
			"", "",
			0, 0,
		},
		{
			"empty",
			"s1", "",
			0, 0,
		},
		{
			"identical",
			"these are the same",
			"these are the same",
			1, 1,
		},
		{
			"disjoint",
			"these strings",
			"just differ",
			0, 0,
		},
		{
			"deletion",
			"one deletion is required",
			"just one deletion is required",
			0.8, 0.8,
		},
		{
			"insertion",
			"one insert operation is required",
			"one operation is required",
			0.8, 0.8,
		},
		{
			"replacement",
			"one replacement is required",
			"one operation is required",
			0.75, 0.75,
		},
		{
			"similar",
			"now these strings are somewhat similar I guess",
			"these two strings are similar I guess",
			0.625, 0.625,
		},
		{
			"caps",
			"we don't care about CAps",
			"We DON'T care about caps",
			1, 1,
		},
		{
			"punctuation_deletion",
			"punctuation doesn't matter much",
			"punctuation doesn't matter much !!",
			0.9, 0.9,
		},
		{
			"punctuation_insertion",
			"who cares ¿ about ? punctuation",
			"who cares about punctuation",
			0.82, 0.84,
		},
		{
			"punctuation_replacement",
			"punctuation doesn't matter as much",
			"punctuation doesnt matter as much!",
			0.8, 0.8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			similarity := levenshtein(computeMessageChunks(&tt.s1), computeMessageChunks(&tt.s2))
			similarity2 := levenshtein(computeMessageChunks(&tt.s2), computeMessageChunks(&tt.s1))
			if similarity < tt.wantMin || similarity > tt.wantMax {
				t.Errorf("levenshtein() = %v, want in range [%v, %v]", similarity, tt.wantMin, tt.wantMax)
			}
			if similarity != similarity2 {
				t.Errorf("levenshtein(%v, %v) is not symmetric", tt.s1, tt.s2)
			}
		})
	}
}
