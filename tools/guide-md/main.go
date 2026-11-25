// Command guide-md turns the user guide (docs/guide/guide.html) into
// Markdown for the GitHub repository's front page. It writes
// README.md and copies the pictures it uses into an img folder beside it,
// so both can be uploaded to GitHub as they are. Run from the project
// folder:
//
//	go run ./tools/guide-md -o release\github
//
// The guide stays the one place to edit: GUIDE.pdf and this README are both
// made from it.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// node is one element of the guide, or a piece of text (tag "").
type node struct {
	tag   string
	attr  map[string]string
	text  string
	kids  []*node
	class string
}

func (n *node) has(class string) bool {
	for _, c := range strings.Fields(n.class) {
		if c == class {
			return true
		}
	}
	return false
}

// parse reads the guide's HTML into a tree. The guide is tidy HTML, which
// Go's XML reader can follow when told about HTML's habits.
func parse(r io.Reader) (*node, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	root := &node{tag: "root"}
	stack := []*node{root}
	for {
		t, err := d.Token()
		if err == io.EOF {
			return root, nil
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := t.(type) {
		case xml.StartElement:
			n := &node{tag: strings.ToLower(t.Name.Local), attr: map[string]string{}}
			for _, a := range t.Attr {
				n.attr[a.Name.Local] = a.Value
			}
			n.class = n.attr["class"]
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			// An end tag closes the nearest open element of its name.
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].tag == name {
					stack = stack[:i]
					break
				}
			}
		case xml.CharData:
			top.kids = append(top.kids, &node{text: string(t)})
		}
	}
}

// writer builds the Markdown.
type writer struct {
	b       strings.Builder
	chapter int
	anchors map[string]string // These are the guide's ids, as GitHub's heading anchors.
	images  []string          // These are the pictures used, to copy.
	// It goes before each picture's path when the pictures stay where they
	// are in the project instead of being copied.
	pictures string
}

var spaces = regexp.MustCompile(`\s+`)

// inline turns a run of text and small elements into one line of Markdown.
func (w *writer) inline(n *node) string {
	if n.tag == "" {
		return spaces.ReplaceAllString(n.text, " ")
	}
	var in strings.Builder
	for _, k := range n.kids {
		in.WriteString(w.inline(k))
	}
	s := in.String()
	wrap := func(mark string) string {
		t := strings.TrimSpace(s)
		if t == "" {
			return s
		}
		// The marks hug the words, with the spaces kept outside them.
		lead := s[:len(s)-len(strings.TrimLeft(s, " "))]
		trail := s[len(strings.TrimRight(s, " ")):]
		return lead + mark + t + mark + trail
	}
	switch n.tag {
	case "b", "strong":
		return wrap("**")
	case "i", "em":
		return wrap("*")
	case "s":
		return wrap("~~")
	case "code":
		return "`" + strings.TrimSpace(textOf(n)) + "`"
	case "br":
		return "<br>"
	case "a":
		href := n.attr["href"]
		if strings.HasPrefix(href, "#") {
			if a, ok := w.anchors[href[1:]]; ok {
				href = "#" + a
			}
		}
		return "[" + strings.TrimSpace(s) + "](" + href + ")"
	case "img", "svg", "script", "style":
		return ""
	}
	return s
}

// textOf is the plain text inside n.
func textOf(n *node) string {
	if n.tag == "" {
		return n.text
	}
	var b strings.Builder
	for _, k := range n.kids {
		b.WriteString(textOf(k))
	}
	return b.String()
}

// isBlock reports whether an element starts its own block.
func isBlock(tag string) bool {
	switch tag {
	case "p", "div", "pre", "ol", "ul", "table", "figure", "dl", "h1", "h2", "h3", "h4", "section", "img":
		return true
	}
	return false
}

// para writes one paragraph, if it holds any words.
func (w *writer) para(prefix, s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	w.b.WriteString(prefix + s + "\n\n")
}

// slug is the anchor GitHub gives a heading.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '-':
			b.WriteRune('-')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r > 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// headingText is a heading as the README writes it: chapters are numbered,
// as in the PDF.
func (w *writer) headingText(n *node, count bool) string {
	t := strings.TrimSpace(w.inline(n))
	if n.tag == "h2" && !n.has("nonum") {
		if count {
			w.chapter++
		}
		t = fmt.Sprintf("%d. %s", w.chapter, t)
	}
	return t
}

// collectAnchors finds, for each id in the guide, the GitHub anchor of the
// heading it belongs to, so links inside the guide keep working.
func (w *writer) collectAnchors(n *node, chapter *int) {
	for _, k := range n.kids {
		if k.tag == "" {
			continue
		}
		if k.tag == "h2" && !k.has("nonum") {
			*chapter++
		}
		if id := k.attr["id"]; id != "" {
			if h := firstHeading(k); h != nil {
				t := strings.TrimSpace(spaces.ReplaceAllString(textOf(h), " "))
				if h.tag == "h2" && !h.has("nonum") {
					t = fmt.Sprintf("%d. %s", *chapter+boolInt(k.tag == "section"), t)
				}
				w.anchors[id] = slug(t)
			}
		}
		w.collectAnchors(k, chapter)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// firstHeading is the first h2, h3 or h4 inside n.
func firstHeading(n *node) *node {
	for _, k := range n.kids {
		switch k.tag {
		case "h2", "h3", "h4":
			return k
		}
		if h := firstHeading(k); h != nil {
			return h
		}
	}
	return nil
}

// blocks writes the block elements inside n, gathering loose text and small
// elements into paragraphs.
func (w *writer) blocks(n *node, prefix string) {
	var run strings.Builder
	flush := func() {
		w.para(prefix, run.String())
		run.Reset()
	}
	for _, k := range n.kids {
		if k.tag == "" || !isBlock(k.tag) {
			run.WriteString(w.inline(k))
			continue
		}
		flush()
		w.block(k, prefix)
	}
	flush()
}

// block writes one block element.
func (w *writer) block(n *node, prefix string) {
	switch n.tag {
	case "section":
		if n.has("cover") {
			w.cover(n)
			return
		}
		if h := firstHeading(n); h != nil && h.has("nonum") {
			return // The Contents page is replaced by the list at the top.
		}
		w.blocks(n, prefix)
	case "h1":
		w.para(prefix, "# "+w.inline(n))
	case "h2":
		w.para(prefix, "## "+w.headingText(n, true))
	case "h3":
		w.para(prefix, "### "+w.inline(n))
	case "h4":
		w.para(prefix, "#### "+w.inline(n))
	case "p":
		w.para(prefix, w.inline(n))
	case "pre":
		w.b.WriteString(prefix + "```text\n")
		for _, line := range strings.Split(strings.Trim(textOf(n), "\n"), "\n") {
			w.b.WriteString(prefix + line + "\n")
		}
		w.b.WriteString(prefix + "```\n\n")
	case "ol", "ul":
		i := 0
		for _, li := range n.kids {
			if li.tag != "li" {
				continue
			}
			i++
			mark := "- "
			if n.tag == "ol" {
				mark = fmt.Sprintf("%d. ", i)
			}
			w.listItem(li, prefix, mark)
		}
		w.b.WriteString("\n")
	case "table":
		w.table(n, prefix)
	case "dl":
		w.definitions(n, prefix)
	case "figure":
		for _, k := range n.kids {
			if k.tag == "img" {
				w.image(k.attr["src"], prefix)
			}
		}
		for _, k := range n.kids {
			if k.tag == "figcaption" {
				w.para(prefix, "*"+strings.TrimSpace(w.inline(k))+"*")
			}
		}
	case "img":
		w.image(n.attr["src"], prefix)
	case "div":
		switch {
		case n.has("tip"):
			w.alert("TIP", n, prefix)
		case n.has("note"):
			w.alert("NOTE", n, prefix)
		case n.has("warn"):
			w.alert("WARNING", n, prefix)
		default:
			w.blocks(n, prefix)
		}
	}
}

// listItem writes one list item; blocks inside it are indented under it.
func (w *writer) listItem(li *node, prefix, mark string) {
	var first strings.Builder
	var rest []*node
	for _, k := range li.kids {
		if len(rest) == 0 && (k.tag == "" || !isBlock(k.tag)) {
			first.WriteString(w.inline(k))
			continue
		}
		rest = append(rest, k)
	}
	w.b.WriteString(prefix + mark + strings.TrimSpace(first.String()) + "\n")
	inner := prefix + strings.Repeat(" ", len(mark))
	for _, k := range rest {
		if k.tag == "" || !isBlock(k.tag) {
			if t := strings.TrimSpace(w.inline(k)); t != "" {
				w.b.WriteString("\n" + inner + t + "\n")
			}
			continue
		}
		w.b.WriteString("\n")
		w.block(k, inner)
	}
}

// cell is a table cell's text, safe inside a Markdown table.
func (w *writer) cell(n *node) string {
	return strings.ReplaceAll(strings.TrimSpace(w.inline(n)), "|", `\|`)
}

func (w *writer) table(n *node, prefix string) {
	var rows [][]string
	var walk func(*node)
	walk = func(n *node) {
		for _, k := range n.kids {
			if k.tag == "tr" {
				var row []string
				for _, c := range k.kids {
					if c.tag == "td" || c.tag == "th" {
						row = append(row, w.cell(c))
					}
				}
				rows = append(rows, row)
				continue
			}
			walk(k)
		}
	}
	walk(n)
	if len(rows) == 0 {
		return
	}
	w.tableRows(rows, prefix)
}

// tableRows writes rows as a Markdown table, the first row as its heading.
func (w *writer) tableRows(rows [][]string, prefix string) {
	cols := len(rows[0])
	line := func(r []string) {
		for len(r) < cols {
			r = append(r, "")
		}
		w.b.WriteString(prefix + "| " + strings.Join(r[:cols], " | ") + " |\n")
	}
	line(rows[0])
	w.b.WriteString(prefix + "|" + strings.Repeat(" --- |", cols) + "\n")
	for _, r := range rows[1:] {
		line(r)
	}
	w.b.WriteString("\n")
}

// definitions writes a list of terms as a two-column table.
func (w *writer) definitions(n *node, prefix string) {
	head := []string{"Term", "Meaning"}
	if n.has("parts") {
		head = []string{"Part", "What it means"}
	}
	rows := [][]string{head}
	var term string
	for _, k := range n.kids {
		switch k.tag {
		case "dt":
			term = w.cell(k)
		case "dd":
			rows = append(rows, []string{term, w.cell(k)})
		}
	}
	w.tableRows(rows, prefix)
}

// alert writes a tip, note or warning as a GitHub alert box.
func (w *writer) alert(kind string, n *node, prefix string) {
	var inner writer
	inner.anchors = w.anchors
	inner.blocks(n, "")
	w.images = append(w.images, inner.images...)
	w.b.WriteString(prefix + "> [!" + kind + "]\n")
	for _, line := range strings.Split(strings.TrimRight(inner.b.String(), "\n"), "\n") {
		w.b.WriteString(strings.TrimRight(prefix+"> "+line, " ") + "\n")
	}
	w.b.WriteString("\n")
}

func (w *writer) image(src, prefix string) {
	if src == "" {
		return
	}
	w.images = append(w.images, src)
	w.b.WriteString(prefix + "![](" + w.pictures + src + ")\n\n")
}

// cover writes the title, the tag line and the cover picture.
func (w *writer) cover(n *node) {
	var walk func(*node)
	walk = func(n *node) {
		for _, k := range n.kids {
			switch {
			case k.tag == "h1":
				w.para("", "# "+w.inline(k))
			case k.tag == "p" && k.has("cover-sub"):
				w.para("", "**"+strings.TrimSpace(w.inline(k))+"**")
			case k.tag == "p" && k.has("cover-tag"):
				w.para("", "*"+strings.TrimSpace(w.inline(k))+"*")
			case k.tag == "p" && k.has("cover-meta"):
				w.para("", strings.ReplaceAll(w.inline(k), "<br>", " · "))
			case k.tag == "img":
				w.image(k.attr["src"], "")
			case k.tag != "":
				walk(k)
			}
		}
	}
	walk(n)
}

// contents writes the list of chapters, linked to their headings.
func (w *writer) contents(root *node) {
	w.b.WriteString("## Contents\n\n")
	n := 0
	var walk func(*node)
	walk = func(x *node) {
		for _, k := range x.kids {
			if k.tag == "h2" && !k.has("nonum") {
				n++
				t := fmt.Sprintf("%d. %s", n, strings.TrimSpace(spaces.ReplaceAllString(textOf(k), " ")))
				fmt.Fprintf(&w.b, "- [%s](#%s)\n", t, slug(t))
				continue
			}
			walk(k)
		}
	}
	walk(root)
	w.b.WriteString("\n")
}

func main() {
	out := flag.String("o", filepath.Join("release", "github"), "the folder to write README.md and img into")
	in := flag.String("guide", filepath.Join("docs", "guide", "guide.html"), "the guide to read")
	inPlace := flag.Bool("inplace", false, "point at the pictures where they are in the project, instead of copying them")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		fail(err)
	}
	root, err := parse(f)
	f.Close()
	if err != nil {
		fail(err)
	}
	w := &writer{anchors: map[string]string{}}
	if *inPlace {
		w.pictures = filepath.ToSlash(filepath.Dir(*in)) + "/"
	}
	chapter := 0
	w.collectAnchors(root, &chapter)

	// The cover comes first, then the list of chapters, then the chapters.
	body := root
	for _, k := range root.kids {
		if k.tag == "html" {
			body = k
		}
	}
	for _, k := range body.kids {
		if k.tag == "body" {
			body = k
		}
	}
	for _, k := range body.kids {
		if k.tag == "section" && k.has("cover") {
			w.cover(k)
		}
	}
	w.contents(body)
	for _, k := range body.kids {
		if k.tag == "section" && !k.has("cover") {
			w.block(k, "")
		}
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	text := strings.TrimRight(w.b.String(), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(*out, "README.md"), []byte(text), 0o644); err != nil {
		fail(err)
	}
	// Only the pictures the guide uses are copied, and none when they stay
	// in place.
	seen := map[string]bool{}
	for _, src := range w.images {
		if seen[src] {
			continue
		}
		seen[src] = true
		if *inPlace {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(*in), filepath.FromSlash(src)))
		if err != nil {
			fail(err)
		}
		dest := filepath.Join(*out, filepath.FromSlash(src))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			fail(err)
		}
	}
	fmt.Printf("Wrote %s with %d pictures.\n", filepath.Join(*out, "README.md"), len(seen))
}

// fail says what went wrong and stops.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "guide-md:", err)
	os.Exit(1)
}
