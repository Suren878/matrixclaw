package repomap

import (
	"slices"
	"strconv"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
)

// maxLineRunes cuts long definition lines in the map.
const maxLineRunes = 120

// fit renders the prefix of ranked whose map lands within 15% of budget
// tokens, or else the largest one under it, by Aider's binary search; it
// returns the map, its tokens and the prefix length.
func fit(paths []string, tags []fileTags, ranked []rankedTag, budget int) (string, int, int) {
	best, bestTokens, bestCount := "", 0, 0
	low, high := 0, len(ranked)
	middle := min(budget/25, len(ranked))
	for low <= high {
		text := render(paths, tags, ranked[:middle])
		tokens := agentcontext.EstimateTextTokens(text)
		off := float64(abs(tokens-budget)) / float64(budget)
		if tokens <= budget && tokens > bestTokens || off < 0.15 {
			best, bestTokens, bestCount = text, tokens, middle
			if off < 0.15 {
				break
			}
		}
		if tokens < budget {
			low = middle + 1
		} else {
			high = middle - 1
		}
		middle = (low + high) / 2
	}
	return best, bestTokens, bestCount
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// render draws the map of the given tags: each file, in the order it first
// appears, with its chosen definition lines in line order, numbered, and the
// enclosing definitions of indented ones; ⋮ marks skipped lines.
func render(paths []string, tags []fileTags, ranked []rankedTag) string {
	var order []int
	chosen := map[int][]int{}
	for _, tag := range ranked {
		if _, seen := chosen[tag.file]; !seen {
			order = append(order, tag.file)
			chosen[tag.file] = nil
		}
		if tag.def >= 0 {
			chosen[tag.file] = append(chosen[tag.file], tag.def)
		}
	}
	var out strings.Builder
	for _, file := range order {
		defs := chosen[file]
		if len(defs) == 0 {
			out.WriteString(paths[file] + "\n")
			continue
		}
		out.WriteString(paths[file] + ":\n")
		all := tags[file].defs
		lines := map[int]string{}
		for _, i := range defs {
			lines[all[i].line] = all[i].text
			child := i
			for parent := i - 1; parent >= 0 && all[child].indent > 0; parent-- {
				if all[parent].indent < all[child].indent {
					lines[all[parent].line] = all[parent].text
					child = parent
				}
			}
		}
		numbers := make([]int, 0, len(lines))
		for line := range lines {
			numbers = append(numbers, line)
		}
		slices.Sort(numbers)
		for k, line := range numbers {
			if k > 0 && line > numbers[k-1]+1 {
				out.WriteString("⋮\n")
			}
			text := lines[line]
			if runes := []rune(text); len(runes) > maxLineRunes {
				text = string(runes[:maxLineRunes]) + "…"
			}
			out.WriteString(strconv.Itoa(line) + "│" + text + "\n")
		}
	}
	return out.String()
}
