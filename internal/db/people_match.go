package db

import (
	"strings"
	"unicode/utf8"
)

const (
	nameExactScore      = 100
	namePrefixScore     = 90
	nameSubstringScore  = 80
	nameFuzzyScore      = 70
	nameInitialScore    = 50
	nameTypoPenalty     = 5
	nameImpossibleScore = -10000
	nameOmittedPenalty  = 15
)

// Adjacent swaps count as one typo. Inputs are bounded before reaching this helper.
func nameDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	d := make([][]int, len(x)+1)
	for i := range d {
		d[i] = make([]int, len(y)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			cost := 0
			if x[i-1] != y[j-1] {
				cost = 1
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(x)][len(y)]
}

func nameTokenScore(query, candidate string) int {
	queryLength, candidateLength := utf8.RuneCountInString(query), utf8.RuneCountInString(candidate)
	if query == candidate {
		return nameExactScore
	}
	if strings.HasPrefix(candidate, query) {
		if queryLength == 1 {
			return nameInitialScore
		}
		return namePrefixScore
	}
	if queryLength >= 3 && strings.Contains(candidate, query) {
		return nameSubstringScore
	}
	if queryLength < 4 || candidateLength < 4 || queryLength > 32 || candidateLength > 32 {
		return 0
	}
	threshold := 1
	if queryLength >= 7 && candidateLength >= 7 {
		threshold = 2
	}
	if queryLength-candidateLength > threshold || candidateLength-queryLength > threshold {
		return 0
	}
	if distance := nameDistance(query, candidate); distance <= threshold {
		return nameFuzzyScore - distance*nameTypoPenalty
	}
	return 0
}

// Match each query token to a distinct name token, allowing surname-first queries.
func nameMatch(query, candidate []string) int {
	// A full Russian name may include a patronymic absent from a two-part profile.
	if len(query) == 3 && len(candidate) == 2 {
		best := 0
		for omitted := range query {
			pair := append(append([]string{}, query[:omitted]...), query[omitted+1:]...)
			best = max(best, nameMatch(pair, candidate)-nameOmittedPenalty)
		}
		return best
	}
	if len(query) == 0 || len(query) > len(candidate) {
		return 0
	}
	scores := namePairScores(query, candidate)
	memo := make(map[uint16]int)
	var match func(int, uint16) int
	match = func(at int, used uint16) int {
		if at == len(query) {
			return 0
		}
		if score, ok := memo[used]; ok {
			return score
		}
		best := nameImpossibleScore
		for j := range candidate {
			bit := uint16(1) << j
			if used&bit != 0 {
				continue
			}
			if next := scores[at][j]; next > 0 {
				best = max(best, next+match(at+1, used|bit))
			}
		}
		memo[used] = best
		return best
	}
	return max(0, match(0, 0)/len(query))
}

// Each token pair has the same score in every assignment state.
func namePairScores(query, candidate []string) [][]int {
	scores := make([][]int, len(query))
	for i, token := range query {
		scores[i] = make([]int, len(candidate))
		for j, other := range candidate {
			scores[i][j] = nameTokenScore(token, other)
		}
	}
	return scores
}
