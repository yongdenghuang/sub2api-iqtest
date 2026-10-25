package iqtest

import (
	"strings"
	"testing"
)

const goodSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 300">
<rect width="400" height="300" fill="#cfe8ff"/>
<circle cx="100" cy="220" r="40" fill="none" stroke="#222"/>
<circle cx="300" cy="220" r="42" fill="none" stroke="#222"/>
<ellipse cx="200" cy="120" rx="50" ry="30" fill="#f5f5f5"/>
<path d="M240 110 L330 100 L250 130 Z" fill="#f4a261"/>
<path d="M100 220 L200 220 L300 220" stroke="#333" fill="none"/>
<path d="M190 150 L200 220" stroke="#333"/>
<rect x="180" y="140" width="40" height="10" fill="#555"/>
<line x1="100" y1="220" x2="180" y2="150" stroke="#333"/>
<polygon points="10,10 20,10 15,20" fill="#e76f51"/>
</svg>`

func shapes(n int) string { return strings.Repeat(`<circle cx="5" cy="5" r="1"/>`, n) }

func TestCheckSVG(t *testing.T) {
	inject := func(extra string) string { return strings.Replace(goodSVG, "</svg>", extra+"</svg>", 1) }

	cases := []struct {
		name  string
		reply string
		opts  SVGOptions
		want  SVGFailure // "" = pass
	}{
		{"good fenced", "好的：\n```svg\n" + goodSVG + "\n```\n希望你喜欢 &copy;", SVGOptions{}, ""},
		{"good raw", goodSVG, SVGOptions{}, ""},
		{"good then second svg", goodSVG + "\n" + goodSVG, SVGOptions{}, ""},
		{"html entity inside", inject(`<text x="1" y="1">&copy;</text>`), SVGOptions{}, ""},

		{"no svg", "抱歉，我无法绘制图片。", SVGOptions{}, SVGFailNoSVG},
		{"svg only in think", "<think><svg viewBox='0 0 1 1'></svg></think>没有", SVGOptions{}, SVGFailNoSVG},
		{"unclosed", `<svg viewBox="0 0 10 10">` + shapes(10), SVGOptions{}, SVGFailUnclosed},
		{"mismatched tags", `<svg viewBox="0 0 10 10"><g>` + shapes(10) + `</svg>`, SVGOptions{}, SVGFailParse},
		{"no size", `<svg xmlns="http://www.w3.org/2000/svg">` + shapes(10) + `</svg>`, SVGOptions{}, SVGFailNoSize},
		{"too few shapes", `<svg viewBox="0 0 10 10">` + shapes(3) + `</svg>`, SVGOptions{}, SVGFailTooFewShapes},

		{"script", inject(`<script>alert(1)</script>`), SVGOptions{}, SVGFailUnsafe},
		{"onload", strings.Replace(goodSVG, "<svg ", `<svg onload="x()" `, 1), SVGOptions{}, SVGFailUnsafe},
		{"external use", inject(`<use href="http://evil.example/x.svg#a"/>`), SVGOptions{}, SVGFailUnsafe},
		{"foreignObject", inject(`<foreignObject><div/></foreignObject>`), SVGOptions{}, SVGFailUnsafe},
		{"image element", inject(`<image href="#a"/>`), SVGOptions{}, SVGFailUnsafe},
		{"css external url", inject(`<rect width="1" height="1" style="fill:url(http://evil.example/a)"/>`), SVGOptions{}, SVGFailUnsafe},
		{"css fragment url ok", inject(`<rect width="1" height="1" style="fill:url(#g)"/>`), SVGOptions{}, ""},

		{"too large bytes", goodSVG, SVGOptions{MaxBytes: 100}, SVGFailTooLarge},
		{"too many elements", goodSVG, SVGOptions{MaxElements: 5}, SVGFailTooLarge},
		{"score gate", `<svg viewBox="0 0 10 10">` + shapes(8) + `</svg>`, SVGOptions{MinScore: 90}, SVGFailLowScore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := CheckSVG(tc.reply, tc.opts)
			if res.Failure != tc.want {
				t.Fatalf("failure = %q (%s), want %q", res.Failure, res.Detail, tc.want)
			}
			if res.Passed != (tc.want == "") {
				t.Fatalf("passed = %v, want %v", res.Passed, tc.want == "")
			}
		})
	}
}

func TestCheckSVGMetrics(t *testing.T) {
	res := CheckSVG(goodSVG, SVGOptions{})
	if !res.Passed {
		t.Fatalf("expected pass, got %q (%s)", res.Failure, res.Detail)
	}
	if res.Metrics.Shapes != 10 || res.Metrics.Paths != 3 {
		t.Fatalf("unexpected metrics: %+v", res.Metrics)
	}
	if !res.Metrics.WheelPair {
		t.Fatalf("expected wheel pair, metrics: %+v", res.Metrics)
	}
	if res.Score < 60 {
		t.Fatalf("score = %d, want >= 60", res.Score)
	}
	if strings.Contains(res.SVG, "</svg>\n<svg") {
		t.Fatalf("extracted SVG should stop at the first root close")
	}
}
