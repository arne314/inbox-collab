package textprocessor

import (
	"unicode"
)

// group of non whitespace adjacent characters
type chunk struct {
	start     int
	end       int
	norm      string // normalized content of this chunk
	normPunct string // same as norm but including punctuation
}

// collection of chunks
type message struct {
	content *string
	chunks  []*chunk
}

func chunkToString(message *message, chunk *chunk) string {
	return (*message.content)[chunk.start:chunk.end]
}

func computeMessageChunks(s *string) *message {
	chunks := make([]*chunk, 0, 10)
	result := &message{content: s}
	start := 0
	space := false
	prevSpace := true

	for i, r := range *s + " " {
		space = unicode.IsSpace(r)
		if prevSpace {
			start = i
		} else if space {
			add := &chunk{start: start, end: i}
			content := chunkToString(result, add)
			add.norm = NormalizeText(content, false)
			add.normPunct = NormalizeText(content, true)
			chunks = append(chunks, add)
		}
		prevSpace = space
	}
	result.chunks = chunks
	return result
}

type direction byte

const (
	stop direction = iota
	diagonal
	top
	left
)

// compute best alignment of block inside base string
func smithWaterman(base *message, block *message) (similarity float32, start int, end int) {
	n := len(base.chunks)
	m := len(block.chunks)
	if n == 0 || m == 0 {
		return 0, 0, 0
	}

	// init matrices
	score := make([][]float32, n)
	trace := make([][]direction, n)
	for i := range n {
		score[i] = make([]float32, m)
		trace[i] = make([]direction, m)
	}

	// fill matrices
	var baseChunk *chunk
	var blockChunk *chunk
	var valueTop, valueLeft, valueDiag float32
	var bestI, bestJ int
	for i := range n {
		baseChunk = base.chunks[i]
		for j := range m {
			blockChunk = block.chunks[j]
			// we consider a gap in the base worse than a gap in the block
			valueTop = -0.5
			valueLeft = -2
			if baseChunk.normPunct == blockChunk.normPunct {
				valueDiag = 1
			} else if baseChunk.norm == blockChunk.norm {
				valueDiag = 0.8
			} else {
				valueDiag = -1
			}
			if i-1 >= 0 {
				valueTop += score[i-1][j]
				// we consider additional words worse than other additional chunks
				if base.chunks[i-1].norm == "" {
					valueTop += 0.4
				}
			}
			if j-1 >= 0 {
				valueLeft += score[i][j-1]
				// see above
				if block.chunks[j-1].norm == "" {
					valueLeft += 0.4
				}
			}
			if i-1 >= 0 && j-1 >= 0 {
				valueDiag += score[i-1][j-1]
			}
			score[i][j] = max(0, valueTop, valueLeft, valueDiag)
			switch score[i][j] {
			case 0: // stop is the initial value
			case valueDiag:
				trace[i][j] = diagonal
			case valueTop:
				trace[i][j] = top
			case valueLeft:
				trace[i][j] = left
			}
			if score[i][j] >= score[bestI][bestJ] {
				bestI = i
				bestJ = j
			}
		}
	}

	if score[bestI][bestJ] == 0 {
		return 0, 0, 0
	}

	// traceback
	i, j := bestI, bestJ
	prevI, prevJ := i, j
	for i >= 0 && j >= 0 && trace[i][j] != stop {
		prevI, prevJ = i, j
		switch trace[i][j] {
		case diagonal:
			i--
			j--
		case top:
			i--
		case left:
			j--
		}
	}
	i, j = prevI, prevJ
	return score[bestI][bestJ] / float32(m), base.chunks[i].start, base.chunks[bestI].end
}
