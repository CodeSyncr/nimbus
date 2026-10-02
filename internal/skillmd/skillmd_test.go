package skillmd

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name, in, wantName, wantDesc, wantBody string
	}{
		{"plain", "---\nname: refunds\ndescription: Issue refunds.\n---\nBody text.\n", "refunds", "Issue refunds.", "Body text."},
		{"folded", "---\nname: refunds\ndescription: >\n  Issue refunds under\n  the policy.\n---\n\nBody", "refunds", "Issue refunds under the policy.", "Body"},
		{"multi-line plain", "---\nname: x\ndescription: one\n  two\n---\nB", "x", "one two", "B"},
		{"crlf and bom", "\uFEFF---\r\nname: x\r\ndescription: d\r\n---\r\nB\r\n", "x", "d", "B"},
		{"quoted with colon", "---\nname: x\ndescription: \"Use when: a refund is asked for\"\n---\nB", "x", "Use when: a refund is asked for", "B"},
		{"no body", "---\nname: x\ndescription: d\n---", "x", "d", ""},
	}
	for _, c := range cases {
		name, desc, body, err := Parse(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if name != c.wantName || desc != c.wantDesc || body != c.wantBody {
			t.Errorf("%s: got (%q, %q, %q)", c.name, name, desc, body)
		}
	}
	for _, bad := range []string{"no frontmatter", "---\nname: x\n(no closing fence)", "---\nname: [unclosed\n---\nB"} {
		if _, _, _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}
