package blog

import (
	"bytes"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/trentnix/hyperserver/pkg/components/form"
	"github.com/trentnix/hyperserver/pkg/routing"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const componentMarkerPrefix = "<!-- hyperserver:component "
const componentMarkerSuffix = " -->"

func componentMarker(node ast.Node, source []byte) string {
	block, ok := node.(*ast.HTMLBlock)
	if !ok || block.HTMLBlockType != ast.HTMLBlockType2 || block.Lines().Len() != 1 || block.HasClosure() {
		return ""
	}
	line := block.Lines().At(0)
	name, ok := strings.CutPrefix(strings.TrimRight(string(line.Value(source)), "\r\n"), componentMarkerPrefix)
	if !ok || line.Padding != 0 {
		return ""
	}
	name, ok = strings.CutSuffix(name, componentMarkerSuffix)
	if !ok || name == "" || strings.ContainsAny(name, " \t") {
		return ""
	}
	return name
}

func parsePlacementMarkdown(source []byte) ast.Node {
	return goldmark.DefaultParser().Parse(text.NewReader(source))
}

func validateComponentPlacements(body string, selected map[string]postComponent) (bool, error) {
	if !strings.Contains(body, componentMarkerPrefix) {
		return false, nil
	}
	source := []byte(body)
	document := parsePlacementMarkdown(source)
	seen := make(map[string]bool)
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		kind := componentMarker(node, source)
		if kind == "" {
			continue
		}
		if _, ok := selected[kind]; !ok {
			return false, writingFieldError{contentError("Remove the component marker or add its component before saving."), "content"}
		}
		if seen[kind] {
			return false, writingFieldError{contentError("A component can appear only once. Remove its extra marker."), "content"}
		}
		seen[kind] = true
	}
	return len(seen) > 0, nil
}

func canPlaceComponent(body string, position int) bool {
	// The candidate also catches an unfinished fence or HTML block at EOF.
	marker := componentMarkerPrefix + "position-check" + componentMarkerSuffix + "\n"
	before := body[:position]
	if position == len(body) && position > 0 && body[position-1] != '\n' {
		before += "\n"
	}
	candidate := []byte(before + marker + body[position:])
	parsed := parsePlacementMarkdown(candidate)
	for node := parsed.FirstChild(); node != nil; node = node.NextSibling() {
		if node.Pos() == len(before) && componentMarker(node, candidate) == "position-check" {
			return true
		}
	}
	return false
}

type componentLayout struct {
	Blocks     []int             `json:"blocks"`
	Drops      []int             `json:"drops"`
	Markers    map[string]int    `json:"markers"`
	Separators map[string]string `json:"separators,omitempty"`
	End        *int              `json:"end"`
}

func componentSeparator(node ast.Node, source []byte, lines []string, line int) (string, bool) {
	before, after := node.PreviousSibling(), node.NextSibling()
	if before == nil || after == nil || componentMarker(before, source) != "" || componentMarker(after, source) != "" {
		return "", false
	}
	// A blank line can still join two lists or indented code blocks. It can
	// also absorb an indented block into the preceding list.
	firstList, firstIsList := before.(*ast.List)
	nextList, nextIsList := after.(*ast.List)
	if firstIsList && (nextIsList && firstList.Marker == nextList.Marker || startsIndented(source, after.Pos())) ||
		before.Kind() == ast.KindCodeBlock && after.Kind() == ast.KindCodeBlock {
		return "<!-- -->", true
	}
	return "", line > 0 && line+1 < len(lines) &&
		strings.Trim(lines[line-1], " \t\r") != "" && strings.Trim(lines[line+1], " \t\r") != ""
}

func startsIndented(source []byte, position int) bool {
	start := bytes.LastIndexByte(source[:position], '\n') + 1
	return start < len(source) && (source[start] == ' ' || source[start] == '\t')
}

// Describe component markers and legal destinations together. Drop targets
// include the blank lines before each block.
func layoutComponents(body string) componentLayout {
	result := componentLayout{Blocks: []int{}, Drops: []int{}, Markers: make(map[string]int)}
	bodyLines := strings.Split(body, "\n")
	source := []byte(body)
	document := parsePlacementMarkdown(source)
	previous, line, lastDrop := 0, 0, -1
	requiresBlankLine := false
	addDrops := func(line int) {
		// Blank lines leading up to a block are real drop targets. Keep the
		// first blank line after an HTML block that needs it as a terminator.
		first := line
		for first > 0 && strings.Trim(bodyLines[first-1], " \t\r") == "" {
			first--
		}
		if requiresBlankLine && first < line {
			first++
		}
		for target := max(first, lastDrop+1); target <= line; target++ {
			result.Drops = append(result.Drops, target)
		}
		lastDrop = line
	}
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		start := strings.LastIndexByte(body[:node.Pos()], '\n') + 1
		line += strings.Count(body[previous:start], "\n")
		previous = start
		if len(result.Blocks) == 0 || result.Blocks[len(result.Blocks)-1] < line {
			result.Blocks = append(result.Blocks, line)
			addDrops(line)
		}
		if kind := componentMarker(node, source); kind != "" {
			if _, exists := result.Markers[kind]; !exists {
				result.Markers[kind] = line
				if separator, keep := componentSeparator(node, source, bodyLines, line); keep {
					if result.Separators == nil {
						result.Separators = make(map[string]string)
					}
					result.Separators[kind] = separator
				}
			}
		}
		html, ok := node.(*ast.HTMLBlock)
		requiresBlankLine = ok && (html.HTMLBlockType == ast.HTMLBlockType6 || html.HTMLBlockType == ast.HTMLBlockType7)
	}
	if canPlaceComponent(body, len(body)) {
		end := len(bodyLines) - 1
		if body != "" && !strings.HasSuffix(body, "\n") {
			end++
		}
		addDrops(end)
		result.End = &end
	} else if requiresBlankLine {
		// A type 6 or 7 HTML block ends at a blank line. The editor
		// inserts that separator when placing a component here.
		end := len(bodyLines)
		if !strings.HasSuffix(body, "\n") {
			end++
		}
		result.Drops = append(result.Drops, end)
		result.End = &end
	}
	return result
}

func (m *module) postComponentPosition(w http.ResponseWriter, r *http.Request) {
	if err := form.ParseWithOptions(r, form.ParseOptions{MaxFieldBytes: routing.MiB}); err != nil {
		http.Error(w, err.Message, err.StatusCode)
		return
	}
	body := r.PostForm.Get("content")
	if !utf8.ValidString(body) {
		http.Error(w, "Markdown must be valid UTF-8.", http.StatusBadRequest)
		return
	}
	writeJSON(w, r, http.StatusOK, layoutComponents(body))
}
