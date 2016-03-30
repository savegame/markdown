//
// Blackfriday Markdown Processor
// Available at http://github.com/russross/blackfriday
//
// Copyright © 2011 Russ Ross <russ@russross.com>.
// Distributed under the Simplified BSD License.
// See README.md for details.
//

//
//
// Markdown parsing and processing
//
//

// Blackfriday markdown processor.
//
// Translates plain text with simple formatting rules into HTML or LaTeX.
package blackfriday

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

const VERSION = "1.4"

type Extensions int

// These are the supported markdown parsing extensions.
// OR these values together to select multiple extensions.
const (
	NoExtensions           Extensions = 0
	NoIntraEmphasis        Extensions = 1 << iota // Ignore emphasis markers inside words
	Tables                                        // Render tables
	FencedCode                                    // Render fenced code blocks
	Autolink                                      // Detect embedded URLs that are not explicitly marked
	Strikethrough                                 // Strikethrough text using ~~test~~
	LaxHTMLBlocks                                 // Loosen up HTML block parsing rules
	SpaceHeaders                                  // Be strict about prefix header rules
	HardLineBreak                                 // Translate newlines into line breaks
	TabSizeEight                                  // Expand tabs to eight spaces instead of four
	Footnotes                                     // Pandoc-style footnotes
	NoEmptyLineBeforeBlock                        // No need to insert an empty line to start a (code, quote, ordered list, unordered list) block
	HeaderIDs                                     // specify header IDs  with {#id}
	Titleblock                                    // Titleblock ala pandoc
	AutoHeaderIDs                                 // Create the header ID from the text
	BackslashLineBreak                            // Translate trailing backslashes into line breaks
	DefinitionLists                               // Render definition lists

	commonHtmlFlags HtmlFlags = UseXHTML | UseSmartypants |
		SmartypantsFractions | SmartypantsDashes | SmartypantsLatexDashes

	commonExtensions Extensions = NoIntraEmphasis | Tables | FencedCode |
		Autolink | Strikethrough | SpaceHeaders | HeaderIDs |
		BackslashLineBreak | DefinitionLists
)

type LinkType int

// These are the possible flag values for the link renderer.
// Only a single one of these values will be used; they are not ORed together.
// These are mostly of interest if you are writing a new output format.
const (
	LinkTypeNotAutolink LinkType = iota
	LinkTypeNormal
	LinkTypeEmail
)

type ListType int

// These are the possible flag values for the ListItem renderer.
// Multiple flag values may be ORed together.
// These are mostly of interest if you are writing a new output format.
const (
	ListTypeOrdered ListType = 1 << iota
	ListTypeDefinition
	ListTypeTerm

	ListItemContainsBlock
	ListItemBeginningOfList
	ListItemEndOfList
)

type TableFlags int

// These are the possible flag values for the table cell renderer.
// Only a single one of these values will be used; they are not ORed together.
// These are mostly of interest if you are writing a new output format.
const (
	TableAlignmentLeft = 1 << iota
	TableAlignmentRight
	TableAlignmentCenter = (TableAlignmentLeft | TableAlignmentRight)
)

// The size of a tab stop.
const (
	TabSizeDefault = 4
	TabSizeDouble  = 8
)

// blockTags is a set of tags that are recognized as HTML block tags.
// Any of these can be included in markdown text without special escaping.
var blockTags = map[string]struct{}{
	"blockquote": struct{}{},
	"del":        struct{}{},
	"div":        struct{}{},
	"dl":         struct{}{},
	"fieldset":   struct{}{},
	"form":       struct{}{},
	"h1":         struct{}{},
	"h2":         struct{}{},
	"h3":         struct{}{},
	"h4":         struct{}{},
	"h5":         struct{}{},
	"h6":         struct{}{},
	"iframe":     struct{}{},
	"ins":        struct{}{},
	"math":       struct{}{},
	"noscript":   struct{}{},
	"ol":         struct{}{},
	"pre":        struct{}{},
	"p":          struct{}{},
	"script":     struct{}{},
	"style":      struct{}{},
	"table":      struct{}{},
	"ul":         struct{}{},

	// HTML5
	"address":    struct{}{},
	"article":    struct{}{},
	"aside":      struct{}{},
	"canvas":     struct{}{},
	"figcaption": struct{}{},
	"figure":     struct{}{},
	"footer":     struct{}{},
	"header":     struct{}{},
	"hgroup":     struct{}{},
	"main":       struct{}{},
	"nav":        struct{}{},
	"output":     struct{}{},
	"progress":   struct{}{},
	"section":    struct{}{},
	"video":      struct{}{},
}

// Renderer is the rendering interface.
// This is mostly of interest if you are implementing a new rendering format.
//
// When a byte slice is provided, it contains the (rendered) contents of the
// element.
//
// When a callback is provided instead, it will write the contents of the
// respective element directly to the output buffer and return true on success.
// If the callback returns false, the rendering function should reset the
// output buffer as though it had never been called.
//
// Currently Html and Latex implementations are provided
type Renderer interface {
	// block-level callbacks
	BlockCode(text []byte, lang string)
	BlockQuote(text []byte)
	BlockHtml(text []byte)
	BeginHeader(level int, id string)
	EndHeader(level int, id string, header []byte)
	HRule()
	BeginList(flags ListType)
	EndList(flags ListType)
	ListItem(text []byte, flags ListType)
	BeginParagraph()
	EndParagraph()
	Table(header []byte, body []byte, columnData []int)
	TableRow(text []byte)
	TableHeaderCell(out *bytes.Buffer, text []byte, flags int)
	TableCell(out *bytes.Buffer, text []byte, flags int)
	BeginFootnotes()
	EndFootnotes()
	FootnoteItem(name, text []byte, flags ListType)
	TitleBlock(text []byte)

	// Span-level callbacks
	AutoLink(link []byte, kind LinkType)
	CodeSpan(text []byte)
	DoubleEmphasis(text []byte)
	Emphasis(text []byte)
	Image(link []byte, title []byte, alt []byte)
	LineBreak()
	Link(link []byte, title []byte, content []byte)
	RawHtmlTag(tag []byte)
	TripleEmphasis(text []byte)
	StrikeThrough(text []byte)
	FootnoteRef(ref []byte, id int)

	// Low-level callbacks
	Entity(entity []byte)
	NormalText(text []byte)

	// Header and footer
	DocumentHeader()
	DocumentFooter()

	GetFlags() HtmlFlags
	CaptureWrites(processor func()) []byte
	CopyWrites(processor func()) []byte
	Write(b []byte) (int, error)
	GetResult() []byte
}

// Callback functions for inline parsing. One such function is defined
// for each character that triggers a response when parsing inline data.
type inlineParser func(p *parser, data []byte, offset int) int

// Parser holds runtime state used by the parser.
// This is constructed by the Markdown function.
type parser struct {
	r              Renderer
	refOverride    ReferenceOverrideFunc
	refs           map[string]*reference
	inlineCallback [256]inlineParser
	flags          Extensions
	nesting        int
	maxNesting     int
	insideLink     bool

	// Footnotes need to be ordered as well as available to quickly check for
	// presence. If a ref is also a footnote, it's stored both in refs and here
	// in notes. Slice is nil if footnotes not enabled.
	notes []*reference

	doc                  *Node
	tip                  *Node // = doc
	oldTip               *Node
	lastMatchedContainer *Node // = doc
	allClosed            bool
}

func (p *parser) getRef(refid string) (ref *reference, found bool) {
	if p.refOverride != nil {
		r, overridden := p.refOverride(refid)
		if overridden {
			if r == nil {
				return nil, false
			}
			return &reference{
				link:     []byte(r.Link),
				title:    []byte(r.Title),
				noteId:   0,
				hasBlock: false,
				text:     []byte(r.Text)}, true
		}
	}
	// refs are case insensitive
	ref, found = p.refs[strings.ToLower(refid)]
	return ref, found
}

func (p *parser) finalize(block *Node) {
	above := block.Parent
	block.open = false
	p.tip = above
}

func (p *parser) addChild(node NodeType, offset uint32) *Node {
	for !p.tip.canContain(node) {
		p.finalize(p.tip)
	}
	newNode := NewNode(node)
	newNode.content = []byte{}
	p.tip.appendChild(newNode)
	p.tip = newNode
	return newNode
}

func (p *parser) closeUnmatchedBlocks() {
	if !p.allClosed {
		for p.oldTip != p.lastMatchedContainer {
			parent := p.oldTip.Parent
			p.finalize(p.oldTip)
			p.oldTip = parent
		}
		p.allClosed = true
	}
}

//
//
// Public interface
//
//

// Reference represents the details of a link.
// See the documentation in Options for more details on use-case.
type Reference struct {
	// Link is usually the URL the reference points to.
	Link string
	// Title is the alternate text describing the link in more detail.
	Title string
	// Text is the optional text to override the ref with if the syntax used was
	// [refid][]
	Text string
}

// ReferenceOverrideFunc is expected to be called with a reference string and
// return either a valid Reference type that the reference string maps to or
// nil. If overridden is false, the default reference logic will be executed.
// See the documentation in Options for more details on use-case.
type ReferenceOverrideFunc func(reference string) (ref *Reference, overridden bool)

// Options represents configurable overrides and callbacks (in addition to the
// extension flag set) for configuring a Markdown parse.
type Options struct {
	// Extensions is a flag set of bit-wise ORed extension bits. See the
	// Extensions flags defined in this package.
	Extensions Extensions

	// ReferenceOverride is an optional function callback that is called every
	// time a reference is resolved.
	//
	// In Markdown, the link reference syntax can be made to resolve a link to
	// a reference instead of an inline URL, in one of the following ways:
	//
	//  * [link text][refid]
	//  * [refid][]
	//
	// Usually, the refid is defined at the bottom of the Markdown document. If
	// this override function is provided, the refid is passed to the override
	// function first, before consulting the defined refids at the bottom. If
	// the override function indicates an override did not occur, the refids at
	// the bottom will be used to fill in the link details.
	ReferenceOverride ReferenceOverrideFunc
}

// MarkdownBasic is a convenience function for simple rendering.
// It processes markdown input with no extensions enabled.
func MarkdownBasic(input []byte) []byte {
	// set up the HTML renderer
	htmlFlags := UseXHTML
	renderer := HtmlRenderer(htmlFlags, "", "")

	// set up the parser
	return MarkdownOptions(input, renderer, Options{Extensions: 0})
}

// Call Markdown with most useful extensions enabled
// MarkdownCommon is a convenience function for simple rendering.
// It processes markdown input with common extensions enabled, including:
//
// * Smartypants processing with smart fractions and LaTeX dashes
//
// * Intra-word emphasis suppression
//
// * Tables
//
// * Fenced code blocks
//
// * Autolinking
//
// * Strikethrough support
//
// * Strict header parsing
//
// * Custom Header IDs
func MarkdownCommon(input []byte) []byte {
	// set up the HTML renderer
	renderer := HtmlRenderer(commonHtmlFlags, "", "")
	return MarkdownOptions(input, renderer, Options{
		Extensions: commonExtensions})
}

// Markdown is the main rendering function.
// It parses and renders a block of markdown-encoded text.
// The supplied Renderer is used to format the output, and extensions dictates
// which non-standard extensions are enabled.
//
// To use the supplied Html or LaTeX renderers, see HtmlRenderer and
// LatexRenderer, respectively.
func Markdown(input []byte, renderer Renderer, extensions Extensions) []byte {
	return MarkdownOptions(input, renderer, Options{
		Extensions: extensions})
}

// MarkdownOptions is just like Markdown but takes additional options through
// the Options struct.
func MarkdownOptions(input []byte, renderer Renderer, opts Options) []byte {
	// no point in parsing if we can't render
	if renderer == nil {
		return nil
	}

	extensions := opts.Extensions

	// fill in the render structure
	p := new(parser)
	p.r = renderer
	p.flags = extensions
	p.refOverride = opts.ReferenceOverride
	p.refs = make(map[string]*reference)
	p.maxNesting = 16
	p.insideLink = false

	docNode := NewNode(Document)
	p.doc = docNode
	p.tip = docNode
	p.oldTip = docNode
	p.lastMatchedContainer = docNode
	p.allClosed = true

	// register inline parsers
	p.inlineCallback['*'] = emphasis
	p.inlineCallback['_'] = emphasis
	if extensions&Strikethrough != 0 {
		p.inlineCallback['~'] = emphasis
	}
	p.inlineCallback['`'] = codeSpan
	p.inlineCallback['\n'] = lineBreak
	p.inlineCallback['['] = link
	p.inlineCallback['<'] = leftAngle
	p.inlineCallback['\\'] = escape
	p.inlineCallback['&'] = entity
	p.inlineCallback['!'] = maybeImage
	p.inlineCallback['^'] = maybeInlineFootnote

	if extensions&Autolink != 0 {
		p.inlineCallback['h'] = maybeAutoLink
		p.inlineCallback['m'] = maybeAutoLink
		p.inlineCallback['f'] = maybeAutoLink
		p.inlineCallback['H'] = maybeAutoLink
		p.inlineCallback['M'] = maybeAutoLink
		p.inlineCallback['F'] = maybeAutoLink
	}

	if extensions&Footnotes != 0 {
		p.notes = make([]*reference, 0)
	}

	first := firstPass(p, input)
	second := secondPass(p, first)
	return second
}

// first pass:
// - extract references
// - expand tabs
// - normalize newlines
// - copy everything else
func firstPass(p *parser, input []byte) []byte {
	var out bytes.Buffer
	tabSize := TabSizeDefault
	if p.flags&TabSizeEight != 0 {
		tabSize = TabSizeDouble
	}
	beg, end := 0, 0
	lastFencedCodeBlockEnd := 0
	for beg < len(input) { // iterate over lines
		if end = isReference(p, input[beg:], tabSize); end > 0 {
			beg += end
		} else { // skip to the next line
			end = beg
			for end < len(input) && input[end] != '\n' && input[end] != '\r' {
				end++
			}

			if p.flags&FencedCode != 0 {
				// track fenced code block boundaries to suppress tab expansion
				// inside them:
				if beg >= lastFencedCodeBlockEnd {
					if i := p.fencedCode(input[beg:], false); i > 0 {
						lastFencedCodeBlockEnd = beg + i
					}
				}
			}

			// add the line body if present
			if end > beg {
				if end < lastFencedCodeBlockEnd { // Do not expand tabs while inside fenced code blocks.
					out.Write(input[beg:end])
				} else {
					expandTabs(&out, input[beg:end], tabSize)
				}
			}
			out.WriteByte('\n')

			if end < len(input) && input[end] == '\r' {
				end++
			}
			if end < len(input) && input[end] == '\n' {
				end++
			}

			beg = end
		}
	}

	// empty input?
	if out.Len() == 0 {
		out.WriteByte('\n')
	}

	return out.Bytes()
}

// second pass: actual rendering
func secondPass(p *parser, input []byte) []byte {
	p.r.DocumentHeader()
	p.block(input)

	if p.flags&Footnotes != 0 && len(p.notes) > 0 {
		p.r.BeginFootnotes()
		flags := ListItemBeginningOfList
		for i := 0; i < len(p.notes); i += 1 {
			ref := p.notes[i]
			var buf bytes.Buffer
			if ref.hasBlock {
				flags |= ListItemContainsBlock
				buf.Write(p.r.CaptureWrites(func() {
					p.block(ref.title)
				}))
			} else {
				buf.Write(p.r.CaptureWrites(func() {
					p.inline(ref.title)
				}))
			}
			p.r.FootnoteItem(ref.link, buf.Bytes(), flags)
			flags &^= ListItemBeginningOfList | ListItemContainsBlock
		}
		p.r.EndFootnotes()
	}

	p.r.DocumentFooter()

	if p.nesting != 0 {
		panic("Nesting level did not end at zero")
	}

	return p.r.GetResult()
}

//
// Link references
//
// This section implements support for references that (usually) appear
// as footnotes in a document, and can be referenced anywhere in the document.
// The basic format is:
//
//    [1]: http://www.google.com/ "Google"
//    [2]: http://www.github.com/ "Github"
//
// Anywhere in the document, the reference can be linked by referring to its
// label, i.e., 1 and 2 in this example, as in:
//
//    This library is hosted on [Github][2], a git hosting site.
//
// Actual footnotes as specified in Pandoc and supported by some other Markdown
// libraries such as php-markdown are also taken care of. They look like this:
//
//    This sentence needs a bit of further explanation.[^note]
//
//    [^note]: This is the explanation.
//
// Footnotes should be placed at the end of the document in an ordered list.
// Inline footnotes such as:
//
//    Inline footnotes^[Not supported.] also exist.
//
// are not yet supported.

// References are parsed and stored in this struct.
type reference struct {
	link     []byte
	title    []byte
	noteId   int // 0 if not a footnote ref
	hasBlock bool
	text     []byte
}

func (r *reference) String() string {
	return fmt.Sprintf("{link: %q, title: %q, text: %q, noteId: %d, hasBlock: %v}",
		r.link, r.title, r.text, r.noteId, r.hasBlock)
}

// Check whether or not data starts with a reference link.
// If so, it is parsed and stored in the list of references
// (in the render struct).
// Returns the number of bytes to skip to move past it,
// or zero if the first line is not a reference.
func isReference(p *parser, data []byte, tabSize int) int {
	// up to 3 optional leading spaces
	if len(data) < 4 {
		return 0
	}
	i := 0
	for i < 3 && data[i] == ' ' {
		i++
	}

	noteId := 0

	// id part: anything but a newline between brackets
	if data[i] != '[' {
		return 0
	}
	i++
	if p.flags&Footnotes != 0 {
		if i < len(data) && data[i] == '^' {
			// we can set it to anything here because the proper noteIds will
			// be assigned later during the second pass. It just has to be != 0
			noteId = 1
			i++
		}
	}
	idOffset := i
	for i < len(data) && data[i] != '\n' && data[i] != '\r' && data[i] != ']' {
		i++
	}
	if i >= len(data) || data[i] != ']' {
		return 0
	}
	idEnd := i

	// spacer: colon (space | tab)* newline? (space | tab)*
	i++
	if i >= len(data) || data[i] != ':' {
		return 0
	}
	i++
	for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	if i < len(data) && (data[i] == '\n' || data[i] == '\r') {
		i++
		if i < len(data) && data[i] == '\n' && data[i-1] == '\r' {
			i++
		}
	}
	for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	if i >= len(data) {
		return 0
	}

	var (
		linkOffset, linkEnd   int
		titleOffset, titleEnd int
		lineEnd               int
		raw                   []byte
		hasBlock              bool
	)

	if p.flags&Footnotes != 0 && noteId != 0 {
		linkOffset, linkEnd, raw, hasBlock = scanFootnote(p, data, i, tabSize)
		lineEnd = linkEnd
	} else {
		linkOffset, linkEnd, titleOffset, titleEnd, lineEnd = scanLinkRef(p, data, i)
	}
	if lineEnd == 0 {
		return 0
	}

	// a valid ref has been found

	ref := &reference{
		noteId:   noteId,
		hasBlock: hasBlock,
	}

	if noteId > 0 {
		// reusing the link field for the id since footnotes don't have links
		ref.link = data[idOffset:idEnd]
		// if footnote, it's not really a title, it's the contained text
		ref.title = raw
	} else {
		ref.link = data[linkOffset:linkEnd]
		ref.title = data[titleOffset:titleEnd]
	}

	// id matches are case-insensitive
	id := string(bytes.ToLower(data[idOffset:idEnd]))

	p.refs[id] = ref

	return lineEnd
}

func scanLinkRef(p *parser, data []byte, i int) (linkOffset, linkEnd, titleOffset, titleEnd, lineEnd int) {
	// link: whitespace-free sequence, optionally between angle brackets
	if data[i] == '<' {
		i++
	}
	linkOffset = i
	for i < len(data) && data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' {
		i++
	}
	if i == len(data) {
		return
	}
	linkEnd = i
	if data[linkOffset] == '<' && data[linkEnd-1] == '>' {
		linkOffset++
		linkEnd--
	}

	// optional spacer: (space | tab)* (newline | '\'' | '"' | '(' )
	for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
		i++
	}
	if i < len(data) && data[i] != '\n' && data[i] != '\r' && data[i] != '\'' && data[i] != '"' && data[i] != '(' {
		return
	}

	// compute end-of-line
	if i >= len(data) || data[i] == '\r' || data[i] == '\n' {
		lineEnd = i
	}
	if i+1 < len(data) && data[i] == '\r' && data[i+1] == '\n' {
		lineEnd++
	}

	// optional (space|tab)* spacer after a newline
	if lineEnd > 0 {
		i = lineEnd + 1
		for i < len(data) && (data[i] == ' ' || data[i] == '\t') {
			i++
		}
	}

	// optional title: any non-newline sequence enclosed in '"() alone on its line
	if i+1 < len(data) && (data[i] == '\'' || data[i] == '"' || data[i] == '(') {
		i++
		titleOffset = i

		// look for EOL
		for i < len(data) && data[i] != '\n' && data[i] != '\r' {
			i++
		}
		if i+1 < len(data) && data[i] == '\n' && data[i+1] == '\r' {
			titleEnd = i + 1
		} else {
			titleEnd = i
		}

		// step back
		i--
		for i > titleOffset && (data[i] == ' ' || data[i] == '\t') {
			i--
		}
		if i > titleOffset && (data[i] == '\'' || data[i] == '"' || data[i] == ')') {
			lineEnd = titleEnd
			titleEnd = i
		}
	}

	return
}

// The first bit of this logic is the same as (*parser).listItem, but the rest
// is much simpler. This function simply finds the entire block and shifts it
// over by one tab if it is indeed a block (just returns the line if it's not).
// blockEnd is the end of the section in the input buffer, and contents is the
// extracted text that was shifted over one tab. It will need to be rendered at
// the end of the document.
func scanFootnote(p *parser, data []byte, i, indentSize int) (blockStart, blockEnd int, contents []byte, hasBlock bool) {
	if i == 0 || len(data) == 0 {
		return
	}

	// skip leading whitespace on first line
	for i < len(data) && data[i] == ' ' {
		i++
	}

	blockStart = i

	// find the end of the line
	blockEnd = i
	for i < len(data) && data[i-1] != '\n' {
		i++
	}

	// get working buffer
	var raw bytes.Buffer

	// put the first line into the working buffer
	raw.Write(data[blockEnd:i])
	blockEnd = i

	// process the following lines
	containsBlankLine := false

gatherLines:
	for blockEnd < len(data) {
		i++

		// find the end of this line
		for i < len(data) && data[i-1] != '\n' {
			i++
		}

		// if it is an empty line, guess that it is part of this item
		// and move on to the next line
		if p.isEmpty(data[blockEnd:i]) > 0 {
			containsBlankLine = true
			blockEnd = i
			continue
		}

		n := 0
		if n = isIndented(data[blockEnd:i], indentSize); n == 0 {
			// this is the end of the block.
			// we don't want to include this last line in the index.
			break gatherLines
		}

		// if there were blank lines before this one, insert a new one now
		if containsBlankLine {
			raw.WriteByte('\n')
			containsBlankLine = false
		}

		// get rid of that first tab, write to buffer
		raw.Write(data[blockEnd+n : i])
		hasBlock = true

		blockEnd = i
	}

	if data[blockEnd-1] != '\n' {
		raw.WriteByte('\n')
	}

	contents = raw.Bytes()

	return
}

//
//
// Miscellaneous helper functions
//
//

// Test if a character is a punctuation symbol.
// Taken from a private function in regexp in the stdlib.
func ispunct(c byte) bool {
	for _, r := range []byte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~") {
		if c == r {
			return true
		}
	}
	return false
}

// Test if a character is a whitespace character.
func isspace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// Test if a character is letter.
func isletter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// Test if a character is a letter or a digit.
// TODO: check when this is looking for ASCII alnum and when it should use unicode
func isalnum(c byte) bool {
	return (c >= '0' && c <= '9') || isletter(c)
}

// Replace tab characters with spaces, aligning to the next TAB_SIZE column.
// always ends output with a newline
func expandTabs(out *bytes.Buffer, line []byte, tabSize int) {
	// first, check for common cases: no tabs, or only tabs at beginning of line
	i, prefix := 0, 0
	slowcase := false
	for i = 0; i < len(line); i++ {
		if line[i] == '\t' {
			if prefix == i {
				prefix++
			} else {
				slowcase = true
				break
			}
		}
	}

	// no need to decode runes if all tabs are at the beginning of the line
	if !slowcase {
		for i = 0; i < prefix*tabSize; i++ {
			out.WriteByte(' ')
		}
		out.Write(line[prefix:])
		return
	}

	// the slow case: we need to count runes to figure out how
	// many spaces to insert for each tab
	column := 0
	i = 0
	for i < len(line) {
		start := i
		for i < len(line) && line[i] != '\t' {
			_, size := utf8.DecodeRune(line[i:])
			i += size
			column++
		}

		if i > start {
			out.Write(line[start:i])
		}

		if i >= len(line) {
			break
		}

		for {
			out.WriteByte(' ')
			column++
			if column%tabSize == 0 {
				break
			}
		}

		i++
	}
}

// Find if a line counts as indented or not.
// Returns number of characters the indent is (0 = not indented).
func isIndented(data []byte, indentSize int) int {
	if len(data) == 0 {
		return 0
	}
	if data[0] == '\t' {
		return 1
	}
	if len(data) < indentSize {
		return 0
	}
	for i := 0; i < indentSize; i++ {
		if data[i] != ' ' {
			return 0
		}
	}
	return indentSize
}

// Create a url-safe slug for fragments
func slugify(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	out := make([]byte, 0, len(in))
	sym := false

	for _, ch := range in {
		if isalnum(ch) {
			sym = false
			out = append(out, ch)
		} else if sym {
			continue
		} else {
			out = append(out, '-')
			sym = true
		}
	}
	var a, b int
	var ch byte
	for a, ch = range out {
		if ch != '-' {
			break
		}
	}
	for b = len(out) - 1; b > 0; b-- {
		if out[b] != '-' {
			break
		}
	}
	return out[a : b+1]
}
