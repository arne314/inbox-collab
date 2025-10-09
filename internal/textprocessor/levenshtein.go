package textprocessor

func levenshtein(message1 *message, message2 *message) (similarity float32) {
	len1 := len(message1.chunks)
	len2 := len(message2.chunks)
	if len1 == 0 || len2 == 0 {
		return 0
	}

	// init previous and current column
	prev := make([]float32, len2+1)
	curr := make([]float32, len2+1)
	for i := range len2 + 1 {
		prev[i] = float32(i)
	}

	// compute distance
	var valueTop, valueLeft, valueDiag float32
	for i := range len1 {
		curr[0] = float32(i) + 1
		for j := range len2 {
			valueTop = curr[j] + 1    // insertion
			valueLeft = prev[j+1] + 1 // deletion
			if message1.chunks[i].norm == "" {
				valueLeft -= 0.5
			}
			if message2.chunks[j].norm == "" {
				valueTop -= 0.5
			}
			if message1.chunks[i].normPunct == message2.chunks[j].normPunct {
				valueDiag = prev[j] // match
			} else if message1.chunks[i].norm == message2.chunks[j].norm {
				valueDiag = prev[j] + 0.5 // replacement of punctuation
			} else {
				valueDiag = prev[j] + 1 // full replacement
			}
			curr[j+1] = min(valueTop, valueLeft, valueDiag)
		}
		prev, curr = curr, prev
	}

	// compute similarity
	return 1 - float32(prev[len2])/float32(max(len1, len2))
}
