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

// compute best alignment of block inside base string
func smithWaterman(base *message, block *message) (similarity float32, start int, end int) {
	n := len(base.chunks)
	m := len(block.chunks)
	if n == 0 || m == 0 {
		return 0, 0, 0
	}

	// init scoring matrix
	matrix := make([][]float32, n)
	for i := range n {
		matrix[i] = make([]float32, m)
	}

	// fill scoring matrix
	var baseChunk *chunk
	var blockChunk *chunk
	var top, left, diag float32
	var bestI, bestJ int
	for i := range n {
		baseChunk = base.chunks[i]
		for j := range m {
			blockChunk = block.chunks[j]
			// we consider a gap in the base worse than a gap in the block
			top = -0.5
			left = -2
			if baseChunk.normPunct == blockChunk.normPunct {
				diag = 1
			} else if baseChunk.norm == blockChunk.norm {
				diag = 0.8
			} else {
				diag = -1
			}
			if i-1 >= 0 {
				top += matrix[i-1][j]
				// we consider additional words worse than other additional chunks
				if base.chunks[i-1].norm == "" {
					top += 0.4
				}
			}
			if j-1 >= 0 {
				left += matrix[i][j-1]
				// see above
				if block.chunks[j-1].norm == "" {
					left += 0.4
				}
			}
			if i-1 >= 0 && j-1 >= 0 {
				diag += matrix[i-1][j-1]
			}
			matrix[i][j] = max(0, top, left, diag)
			if matrix[i][j] >= matrix[bestI][bestJ] {
				bestI = i
				bestJ = j
			}
		}
	}

	if matrix[bestI][bestJ] == 0 {
		return 0, 0, 0
	}

	// traceback
	i, j := bestI, bestJ
traceback:
	for i >= 0 || j >= 0 {
		top, left, diag = 0, 0, 0
		if i-1 >= 0 {
			top = matrix[i-1][j]
		}
		if j-1 >= 0 {
			left = matrix[i][j-1]
		}
		if i-1 >= 0 && j-1 >= 0 {
			diag = matrix[i-1][j-1]
		}
		switch max(0, top, left, diag) {
		case 0:
			break traceback
		case diag:
			i--
			j--
		case top:
			i--
		case left:
			j--
		}
	}
	return matrix[bestI][bestJ] / float32(m), base.chunks[i].start, base.chunks[bestI].end
}
