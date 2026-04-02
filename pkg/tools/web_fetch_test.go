package tools

import (
	"strings"
	"testing"
)

func TestWebFetchStripHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "basic tags",
			input:    "<p>Hello</p> <b>World</b>",
			contains: []string{"Hello", "World"},
			excludes: []string{"<p>", "</p>", "<b>", "</b>"},
		},
		{
			name:     "script removal",
			input:    "<p>Keep</p><script>var x = 1;</script><p>Also keep</p>",
			contains: []string{"Keep", "Also keep"},
			excludes: []string{"<script>", "var x"},
		},
		{
			name:     "style removal",
			input:    "<style>.foo{color:red}</style><div>Content</div>",
			contains: []string{"Content"},
			excludes: []string{"<style>", "color:red"},
		},
		{
			name:     "entity decode",
			input:    "<p>A &amp; B &lt; C &gt; D</p>",
			contains: []string{"A & B < C > D"},
			excludes: []string{"&amp;", "&lt;", "&gt;"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripHTMLTags(tt.input)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("expected result to contain %q, got %q", want, result)
				}
			}
			for _, exclude := range tt.excludes {
				if strings.Contains(result, exclude) {
					t.Errorf("expected result to NOT contain %q, got %q", exclude, result)
				}
			}
		})
	}
}
