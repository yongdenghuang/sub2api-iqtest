package iqtest

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SVGFailure is the reason an SVG failed the static check ("" means passed).
type SVGFailure string

const (
	SVGFailNoSVG        SVGFailure = "no_svg"
	SVGFailUnclosed     SVGFailure = "svg_unclosed"
	SVGFailParse        SVGFailure = "svg_parse_error"
	SVGFailRoot         SVGFailure = "svg_bad_root"
	SVGFailNoSize       SVGFailure = "svg_no_size"
	SVGFailUnsafe       SVGFailure = "svg_unsafe"
	SVGFailTooLarge     SVGFailure = "svg_too_large"
	SVGFailTooFewShapes SVGFailure = "svg_too_few_shapes"
	SVGFailLowScore     SVGFailure = "svg_low_score"
)

// SVGOptions tunes the static check. Zero values select the defaults.
type SVGOptions struct {
	MaxBytes    int // reject larger documents (default 256 KiB, measured from the first <svg)
	MaxElements int // reject documents with more elements (default 5000)
	MinShapes   int // minimum drawing primitives (default 8)
	MinScore    int // minimum heuristic score; 0 disables the score gate
}

func (o SVGOptions) withDefaults() SVGOptions {
	if o.MaxBytes <= 0 {
		o.MaxBytes = 256 << 10
	}
	if o.MaxElements <= 0 {
		o.MaxElements = 5000
	}
	if o.MinShapes <= 0 {
		o.MinShapes = 8
	}
	if o.MinScore < 0 {
		o.MinScore = 0
	}
	return o
}

// SVGMetrics are structural counts collected while parsing.
type SVGMetrics struct {
	Elements    int  `json:"elements"`
	Shapes      int  `json:"shapes"`
	Paths       int  `json:"paths"`
	Groups      int  `json:"groups"`
	Colors      int  `json:"colors"`
	RoundShapes int  `json:"round_shapes"`
	WheelPair   bool `json:"wheel_pair"` // two similar-sized large round shapes (weak signal)
}

// SVGResult is the outcome of the static SVG check.
// Passed depends only on the hard checks (and MinScore if set);
// Score is a heuristic for human comparison.
type SVGResult struct {
	Passed  bool       `json:"passed"`
	Failure SVGFailure `json:"failure,omitempty"`
	Detail  string     `json:"detail,omitempty"`
	Score   int        `json:"score"`
	Metrics SVGMetrics `json:"metrics"`
	SVG     string     `json:"svg,omitempty"` // the extracted document
}

var (
	svgOpenRe    = regexp.MustCompile(`(?i)<svg[\s>/]`)
	svgCloseRe   = regexp.MustCompile(`(?i)</svg\s*>`)
	cssURLRe     = regexp.MustCompile(`(?i)url\(\s*['"]?\s*([^)'"\s]*)`)
	styleColorRe = regexp.MustCompile(`(?i)(?:fill|stroke|stop-color)\s*:\s*([^;]+)`)
)

var forbiddenSVGElements = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "object": true,
	"embed": true, "audio": true, "video": true, "canvas": true, "image": true,
}

var shapeTags = map[string]bool{
	"path": true, "circle": true, "ellipse": true, "rect": true,
	"line": true, "polyline": true, "polygon": true,
}

// CheckSVG statically validates the first <svg> document found in reply.
// An empty reply must be handled by the caller as an infrastructure error.
func CheckSVG(reply string, opts SVGOptions) SVGResult {
	opts = opts.withDefaults()
	var res SVGResult

	loc := svgOpenRe.FindStringIndex(stripReasoning(reply))
	text := stripReasoning(reply)
	if loc == nil {
		res.Failure, res.Detail = SVGFailNoSVG, "no <svg> element in reply"
		return res
	}
	rest := text[loc[0]:]
	if !svgCloseRe.MatchString(rest) {
		res.Failure, res.Detail = SVGFailUnclosed, "no closing </svg>"
		return res
	}
	if len(rest) > opts.MaxBytes {
		res.Failure = SVGFailTooLarge
		res.Detail = fmt.Sprintf("%d bytes exceeds limit %d", len(rest), opts.MaxBytes)
		return res
	}

	m, used, fail, detail := inspectSVG(rest, opts)
	res.Metrics, res.SVG = m, used
	if fail != "" {
		res.Failure, res.Detail = fail, detail
		return res
	}
	if m.Shapes < opts.MinShapes {
		res.Failure = SVGFailTooFewShapes
		res.Detail = fmt.Sprintf("%d shapes, need at least %d", m.Shapes, opts.MinShapes)
		return res
	}
	res.Score = scoreSVG(m)
	if opts.MinScore > 0 && res.Score < opts.MinScore {
		res.Failure = SVGFailLowScore
		res.Detail = fmt.Sprintf("score %d below %d", res.Score, opts.MinScore)
		return res
	}
	res.Passed = true
	return res
}

// inspectSVG parses the first root element of doc and stops when it closes.
func inspectSVG(doc string, opts SVGOptions) (m SVGMetrics, used string, fail SVGFailure, detail string) {
	d := xml.NewDecoder(strings.NewReader(doc))
	d.Strict = true
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }

	colors := map[string]struct{}{}
	var radii []float64
	depth, inStyle := 0, 0

loop:
	for {
		tok, err := d.Token()
		if err != nil {
			return m, "", SVGFailParse, err.Error()
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := strings.ToLower(t.Name.Local)
			if depth == 1 {
				if name != "svg" {
					return m, "", SVGFailRoot, "root element is <" + name + ">"
				}
				if !hasSize(t.Attr) {
					return m, "", SVGFailNoSize, "root <svg> has neither viewBox nor width/height"
				}
			}
			m.Elements++
			if m.Elements > opts.MaxElements {
				return m, "", SVGFailTooLarge, fmt.Sprintf("more than %d elements", opts.MaxElements)
			}
			if forbiddenSVGElements[name] {
				return m, "", SVGFailUnsafe, "forbidden element <" + name + ">"
			}
			if why := unsafeAttrs(t.Attr); why != "" {
				return m, "", SVGFailUnsafe, why
			}
			collectColors(t.Attr, colors)
			if shapeTags[name] {
				m.Shapes++
			}
			switch name {
			case "path":
				m.Paths++
			case "g":
				m.Groups++
			case "circle":
				if r := numAttr(t.Attr, "r"); r > 0 {
					radii = append(radii, r)
				}
			case "ellipse":
				rx, ry := numAttr(t.Attr, "rx"), numAttr(t.Attr, "ry")
				if rx > 0 && ry > 0 {
					radii = append(radii, (rx+ry)/2)
				}
			case "style":
				inStyle++
			}
		case xml.EndElement:
			depth--
			if strings.EqualFold(t.Name.Local, "style") && inStyle > 0 {
				inStyle--
			}
			if depth == 0 {
				used = doc[:d.InputOffset()]
				break loop
			}
		case xml.CharData:
			if inStyle > 0 && unsafeCSS(string(t)) {
				return m, "", SVGFailUnsafe, "unsafe CSS in <style>"
			}
		}
	}

	m.Colors = len(colors)
	m.RoundShapes = len(radii)
	m.WheelPair = hasSimilarPair(radii)
	return m, used, "", ""
}

func unsafeAttrs(attrs []xml.Attr) string {
	for _, a := range attrs {
		local := strings.ToLower(a.Name.Local)
		val := strings.TrimSpace(a.Value)
		lv := strings.ToLower(val)
		switch {
		case strings.HasPrefix(local, "on") && len(local) > 2:
			return "event handler attribute " + local
		case (local == "href" || local == "src") && !strings.HasPrefix(val, "#"):
			return "external reference in " + local
		case strings.Contains(lv, "javascript:"):
			return "javascript: URL in " + local
		case strings.Contains(lv, "url(") || local == "style":
			if unsafeCSS(val) {
				return "unsafe CSS in " + local
			}
		}
	}
	return ""
}

// unsafeCSS rejects @import, expression() and any url() that is not a #fragment.
func unsafeCSS(s string) bool {
	l := strings.ToLower(s)
	if strings.Contains(l, "@import") || strings.Contains(l, "expression(") || strings.Contains(l, "javascript:") {
		return true
	}
	for _, m := range cssURLRe.FindAllStringSubmatch(s, -1) {
		if !strings.HasPrefix(m[1], "#") {
			return true
		}
	}
	return false
}

func hasSize(attrs []xml.Attr) bool {
	var vb, w, h string
	for _, a := range attrs {
		switch strings.ToLower(a.Name.Local) {
		case "viewbox":
			vb = strings.TrimSpace(a.Value)
		case "width":
			w = strings.TrimSpace(a.Value)
		case "height":
			h = strings.TrimSpace(a.Value)
		}
	}
	return vb != "" || (w != "" && h != "")
}

func numAttr(attrs []xml.Attr, local string) float64 {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, local) {
			v := strings.TrimRight(strings.TrimSpace(a.Value), "pxemrt%")
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return 0
			}
			return f
		}
	}
	return 0
}

func collectColors(attrs []xml.Attr, out map[string]struct{}) {
	add := func(v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		switch v {
		case "", "none", "transparent", "currentcolor", "inherit":
			return
		}
		if strings.HasPrefix(v, "url(") {
			return
		}
		out[v] = struct{}{}
	}
	for _, a := range attrs {
		switch strings.ToLower(a.Name.Local) {
		case "fill", "stroke", "stop-color":
			add(a.Value)
		case "style":
			for _, m := range styleColorRe.FindAllStringSubmatch(a.Value, -1) {
				add(m[1])
			}
		}
	}
}

// hasSimilarPair looks for two similar-sized shapes among the six largest
// round shapes (a weak proxy for "two wheels").
func hasSimilarPair(radii []float64) bool {
	if len(radii) < 2 {
		return false
	}
	rs := append([]float64(nil), radii...)
	sort.Float64s(rs)
	if len(rs) > 6 {
		rs = rs[len(rs)-6:]
	}
	for i := 0; i < len(rs); i++ {
		for j := i + 1; j < len(rs); j++ {
			small, big := rs[i], rs[j]
			if small > 0 && big/small <= 1.67 {
				return true
			}
		}
	}
	return false
}

// scoreSVG is a 0-100 heuristic for side-by-side comparison. It never decides
// pass/fail on its own unless SVGOptions.MinScore is set.
func scoreSVG(m SVGMetrics) int {
	score := min(m.Shapes, 20) * 2
	score += min(m.Colors, 4) * 5
	if m.WheelPair {
		score += 25
	}
	score += min(m.Paths, 3) * 5
	return score
}
