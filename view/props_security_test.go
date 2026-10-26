package view

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestPropsAttributeValuesCannotEscape(t *testing.T) {
	for _, value := range []any{`x" onmouseover=alert(1) x="`, []string{`x"`, `onmouseover=alert(1)`}, []any{`x"`, `onmouseover=alert(1)`}} {
		p := NewProps(map[string]any{"title": value, "data-safe": "<&\"'"})
		var b bytes.Buffer
		if err := template.Must(template.New("p").Parse(`<div {{.ToAttrs}}>hello</div>`)).Execute(&b, p); err != nil {
			t.Fatal(err)
		}
		doc, err := html.Parse(strings.NewReader(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "div" {
				if len(n.Attr) != 2 {
					t.Fatalf("injected attributes: %+v", n.Attr)
				}
				for _, a := range n.Attr {
					if a.Key != "title" && a.Key != "data-safe" {
						t.Fatal(a)
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
	}
}

func TestPropsRejectExecutableNamesAndUnsafeURLs(t *testing.T) {
	attrs := string(NewProps(map[string]any{"bad\" onclick": "x", "onclick": "alert(1)", "OnLoad": true, "srcdoc": "<script>x</script>", "data-ok": "yes", "aria-label": "label"}).ToAttrs())
	if strings.Contains(attrs, "onclick") || strings.Contains(attrs, "OnLoad") || strings.Contains(attrs, "srcdoc") || strings.Contains(attrs, "bad") {
		t.Fatal(attrs)
	}
	for _, key := range []string{"href", "src", "action", "formaction", "xlink:href", "srcset"} {
		got := string(NewProps(map[string]any{key: "javascript:alert(1)"}).ToAttrs())
		if strings.Contains(got, "javascript:") || !strings.Contains(got, "ZgotmplZ") {
			t.Fatalf("%s: %s", key, got)
		}
	}
	got := string(NewProps(map[string]any{"href": "https://example.com/?a=1&b=2", "disabled": true, "hidden": false}).ToAttrs())
	if !strings.Contains(got, `href="https://example.com/?a=1&amp;b=2"`) || !strings.Contains(got, "disabled") || strings.Contains(got, "hidden") {
		t.Fatal(got)
	}
}
