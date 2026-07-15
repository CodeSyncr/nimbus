package browser

import (
	"html"
	"regexp"
	"strings"
)

// htmlForm is a parsed <form> with its action, method, and default field values.
type htmlForm struct {
	action  string
	method  string
	fields  map[string]string // name -> default value
	submits []string          // submit button labels/values
	raw     string
}

// hasSubmit reports whether the form has a submit control matching text
// (by button text or input value, case-insensitive substring).
func (f *htmlForm) hasSubmit(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, s := range f.submits {
		if strings.Contains(strings.ToLower(s), t) {
			return true
		}
	}
	return false
}

var (
	reForm     = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	reInput    = regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	reTextarea = regexp.MustCompile(`(?is)<textarea\b([^>]*)>(.*?)</textarea>`)
	reSelect   = regexp.MustCompile(`(?is)<select\b([^>]*)>(.*?)</select>`)
	reOption   = regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
	reButton   = regexp.MustCompile(`(?is)<button\b([^>]*)>(.*?)</button>`)
	reLink     = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	reAttr     = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"`)
	reTag      = regexp.MustCompile(`(?is)<[^>]+>`)
)

// attrs parses HTML attributes from a tag's attribute string.
func attrs(s string) map[string]string {
	m := map[string]string{}
	for _, a := range reAttr.FindAllStringSubmatch(s, -1) {
		m[strings.ToLower(a[1])] = html.UnescapeString(a[2])
	}
	return m
}

// hasFlag reports a boolean attribute's presence (e.g. "checked", "selected").
func hasFlag(s, flag string) bool {
	return regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(flag) + `\b`).MatchString(s)
}

// parseForms extracts all forms from an HTML document.
func parseForms(doc string) []htmlForm {
	var forms []htmlForm
	for _, fm := range reForm.FindAllStringSubmatch(doc, -1) {
		fa := attrs(fm[1])
		inner := fm[2]
		form := htmlForm{
			action: fa["action"],
			method: fa["method"],
			fields: map[string]string{},
			raw:    fm[0],
		}

		// <input>
		for _, in := range reInput.FindAllStringSubmatch(inner, -1) {
			ia := attrs(in[1])
			name := ia["name"]
			typ := strings.ToLower(ia["type"])
			if typ == "submit" || typ == "button" {
				if v := ia["value"]; v != "" {
					form.submits = append(form.submits, v)
				}
				continue
			}
			if name == "" {
				continue
			}
			switch typ {
			case "checkbox", "radio":
				if hasFlag(in[1], "checked") {
					v := ia["value"]
					if v == "" {
						v = "on"
					}
					form.fields[name] = v
				} else if _, ok := form.fields[name]; !ok {
					form.fields[name] = ""
				}
			default:
				form.fields[name] = ia["value"]
			}
		}

		// <textarea>
		for _, ta := range reTextarea.FindAllStringSubmatch(inner, -1) {
			ea := attrs(ta[1])
			if name := ea["name"]; name != "" {
				form.fields[name] = html.UnescapeString(strings.TrimSpace(ta[2]))
			}
		}

		// <select> — default to the selected option, else the first.
		for _, se := range reSelect.FindAllStringSubmatch(inner, -1) {
			sa := attrs(se[1])
			name := sa["name"]
			if name == "" {
				continue
			}
			opts := reOption.FindAllStringSubmatch(se[2], -1)
			chosen, first := "", ""
			for i, op := range opts {
				oa := attrs(op[1])
				val := oa["value"]
				if val == "" {
					val = stripTags(op[2])
				}
				if i == 0 {
					first = val
				}
				if hasFlag(op[1], "selected") {
					chosen = val
				}
			}
			if chosen == "" {
				chosen = first
			}
			form.fields[name] = chosen
		}

		// <button type="submit">Label</button>
		for _, bt := range reButton.FindAllStringSubmatch(inner, -1) {
			ba := attrs(bt[1])
			if t := strings.ToLower(ba["type"]); t != "" && t != "submit" {
				continue
			}
			label := strings.TrimSpace(stripTags(bt[2]))
			if v := ba["value"]; v != "" {
				// A named submit button contributes its own name=value pair.
				if n := ba["name"]; n != "" {
					form.fields[n] = v
				}
				form.submits = append(form.submits, v)
			}
			if label != "" {
				form.submits = append(form.submits, label)
			}
		}

		forms = append(forms, form)
	}
	return forms
}

// findLinkHref returns the href of the first <a> whose visible text contains text.
func findLinkHref(doc, text string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, m := range reLink.FindAllStringSubmatch(doc, -1) {
		label := strings.ToLower(strings.TrimSpace(stripTags(m[2])))
		if strings.Contains(label, t) {
			return attrs(m[1])["href"], true
		}
	}
	return "", false
}

// inputValue returns the value of a named input/textarea/select in the document.
func inputValue(doc, name string) (string, bool) {
	for _, f := range parseForms(doc) {
		if v, ok := f.fields[name]; ok {
			return v, true
		}
	}
	return "", false
}

var reTitle = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)

// pageTitle returns the trimmed <title> text, or "".
func pageTitle(doc string) string {
	m := reTitle.FindStringSubmatch(doc)
	if m == nil {
		return ""
	}
	return stripTags(m[1])
}

// elementText returns the visible text of the first element matching a minimal
// selector: a tag name ("h1"), ".class", or "#id". Same-tag nesting is not
// resolved (first close wins), which is adequate for test assertions.
func elementText(doc, selector string) (string, bool) {
	selector = strings.TrimSpace(selector)
	var open *regexp.Regexp
	switch {
	case strings.HasPrefix(selector, "."):
		cls := regexp.QuoteMeta(selector[1:])
		open = regexp.MustCompile(`(?is)<([a-zA-Z0-9]+)\b[^>]*\bclass\s*=\s*"[^"]*\b` + cls + `\b[^"]*"[^>]*>`)
	case strings.HasPrefix(selector, "#"):
		id := regexp.QuoteMeta(selector[1:])
		open = regexp.MustCompile(`(?is)<([a-zA-Z0-9]+)\b[^>]*\bid\s*=\s*"` + id + `"[^>]*>`)
	default:
		tag := regexp.QuoteMeta(selector)
		open = regexp.MustCompile(`(?is)<(` + tag + `)\b[^>]*>`)
	}
	loc := open.FindStringSubmatchIndex(doc)
	if loc == nil {
		return "", false
	}
	tagName := doc[loc[2]:loc[3]]
	rest := doc[loc[1]:]
	closeIdx := strings.Index(strings.ToLower(rest), "</"+strings.ToLower(tagName)+">")
	if closeIdx < 0 {
		return stripTags(rest), true
	}
	return stripTags(rest[:closeIdx]), true
}

// stripTags removes HTML tags and collapses whitespace, returning visible text.
func stripTags(s string) string {
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
