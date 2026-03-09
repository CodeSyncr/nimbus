package shield

import (
	"crypto/rand"
	"encoding/base64"
	"sort"
	"strings"
)

// CSPBuilder provides a fluent API for assembling a Content Security
// Policy. Each method returns the builder so calls can be chained.
//
//	policy := shield.NewCSP().
//	    DefaultSrc("'self'").
//	    ScriptSrc("'self'", "https://cdn.example.com").
//	    StyleSrc("'self'", "'unsafe-inline'").
//	    ImgSrc("'self'", "data:").
//	    ReportURI("/csp-report")
type CSPBuilder struct {
	directives map[string][]string
	reportOnly bool
}

// NewCSP creates a new Content Security Policy builder.
func NewCSP() *CSPBuilder {
	return &CSPBuilder{directives: make(map[string][]string)}
}

// --------------------------------------------------------------------------
// Fetch directives
// --------------------------------------------------------------------------

func (b *CSPBuilder) DefaultSrc(sources ...string) *CSPBuilder {
	return b.add("default-src", sources)
}

func (b *CSPBuilder) ScriptSrc(sources ...string) *CSPBuilder {
	return b.add("script-src", sources)
}

func (b *CSPBuilder) ScriptSrcElem(sources ...string) *CSPBuilder {
	return b.add("script-src-elem", sources)
}

func (b *CSPBuilder) ScriptSrcAttr(sources ...string) *CSPBuilder {
	return b.add("script-src-attr", sources)
}

func (b *CSPBuilder) StyleSrc(sources ...string) *CSPBuilder {
	return b.add("style-src", sources)
}

func (b *CSPBuilder) StyleSrcElem(sources ...string) *CSPBuilder {
	return b.add("style-src-elem", sources)
}

func (b *CSPBuilder) StyleSrcAttr(sources ...string) *CSPBuilder {
	return b.add("style-src-attr", sources)
}

func (b *CSPBuilder) ImgSrc(sources ...string) *CSPBuilder {
	return b.add("img-src", sources)
}

func (b *CSPBuilder) FontSrc(sources ...string) *CSPBuilder {
	return b.add("font-src", sources)
}

func (b *CSPBuilder) ConnectSrc(sources ...string) *CSPBuilder {
	return b.add("connect-src", sources)
}

func (b *CSPBuilder) MediaSrc(sources ...string) *CSPBuilder {
	return b.add("media-src", sources)
}

func (b *CSPBuilder) ObjectSrc(sources ...string) *CSPBuilder {
	return b.add("object-src", sources)
}

func (b *CSPBuilder) FrameSrc(sources ...string) *CSPBuilder {
	return b.add("frame-src", sources)
}

func (b *CSPBuilder) ChildSrc(sources ...string) *CSPBuilder {
	return b.add("child-src", sources)
}

func (b *CSPBuilder) WorkerSrc(sources ...string) *CSPBuilder {
	return b.add("worker-src", sources)
}

func (b *CSPBuilder) ManifestSrc(sources ...string) *CSPBuilder {
	return b.add("manifest-src", sources)
}

func (b *CSPBuilder) PrefetchSrc(sources ...string) *CSPBuilder {
	return b.add("prefetch-src", sources)
}

// --------------------------------------------------------------------------
// Document directives
// --------------------------------------------------------------------------

func (b *CSPBuilder) BaseURI(sources ...string) *CSPBuilder {
	return b.add("base-uri", sources)
}

func (b *CSPBuilder) Sandbox(flags ...string) *CSPBuilder {
	return b.add("sandbox", flags)
}

// --------------------------------------------------------------------------
// Navigation directives
// --------------------------------------------------------------------------

func (b *CSPBuilder) FormAction(sources ...string) *CSPBuilder {
	return b.add("form-action", sources)
}

func (b *CSPBuilder) FrameAncestors(sources ...string) *CSPBuilder {
	return b.add("frame-ancestors", sources)
}

func (b *CSPBuilder) NavigateTo(sources ...string) *CSPBuilder {
	return b.add("navigate-to", sources)
}

// --------------------------------------------------------------------------
// Reporting directives
// --------------------------------------------------------------------------

func (b *CSPBuilder) ReportURI(uri string) *CSPBuilder {
	b.directives["report-uri"] = []string{uri}
	return b
}

func (b *CSPBuilder) ReportTo(group string) *CSPBuilder {
	b.directives["report-to"] = []string{group}
	return b
}

// --------------------------------------------------------------------------
// Other directives
// --------------------------------------------------------------------------

func (b *CSPBuilder) UpgradeInsecureRequests() *CSPBuilder {
	b.directives["upgrade-insecure-requests"] = nil
	return b
}

func (b *CSPBuilder) BlockAllMixedContent() *CSPBuilder {
	b.directives["block-all-mixed-content"] = nil
	return b
}

// --------------------------------------------------------------------------
// Custom directive
// --------------------------------------------------------------------------

// Directive sets an arbitrary directive. Useful for new directives not
// yet covered by a dedicated method.
func (b *CSPBuilder) Directive(name string, values ...string) *CSPBuilder {
	return b.add(name, values)
}

// --------------------------------------------------------------------------
// Nonce helper
// --------------------------------------------------------------------------

// Nonce generates a cryptographically random base64 nonce value and
// appends 'nonce-<value>' to the given directive. Returns the raw
// nonce string for embedding in a <script nonce="..."> tag.
//
//	nonce := policy.Nonce("script-src")
//	// In template: <script nonce="{{ nonce }}">...</script>
func (b *CSPBuilder) Nonce(directive string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic("shield: crypto/rand failed: " + err.Error())
	}
	value := base64.StdEncoding.EncodeToString(raw)
	b.add(directive, []string{"'nonce-" + value + "'"})
	return value
}

// --------------------------------------------------------------------------
// Serialization
// --------------------------------------------------------------------------

// String serializes the policy into a valid CSP header value.
func (b *CSPBuilder) String() string {
	if len(b.directives) == 0 {
		return ""
	}

	keys := make([]string, 0, len(b.directives))
	for k := range b.directives {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := b.directives[k]
		if len(vals) == 0 {
			parts = append(parts, k)
		} else {
			parts = append(parts, k+" "+strings.Join(vals, " "))
		}
	}
	return strings.Join(parts, "; ")
}

// --------------------------------------------------------------------------
// Presets
// --------------------------------------------------------------------------

// Strict returns a restrictive baseline policy suitable for most
// server-rendered applications.
func Strict() *CSPBuilder {
	return NewCSP().
		DefaultSrc("'self'").
		ScriptSrc("'self'").
		StyleSrc("'self'", "'unsafe-inline'").
		ImgSrc("'self'", "data:").
		FontSrc("'self'").
		ObjectSrc("'none'").
		BaseURI("'self'").
		FormAction("'self'").
		FrameAncestors("'none'").
		UpgradeInsecureRequests()
}

// Relaxed returns a more permissive policy that allows CDN assets and
// inline styles — useful during development or for apps that load
// resources from multiple origins.
func Relaxed() *CSPBuilder {
	return NewCSP().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "https:", "'unsafe-inline'").
		StyleSrc("'self'", "https:", "'unsafe-inline'").
		ImgSrc("'self'", "data:", "https:").
		FontSrc("'self'", "https:", "data:").
		ObjectSrc("'none'").
		BaseURI("'self'").
		FormAction("'self'")
}

func (b *CSPBuilder) add(directive string, values []string) *CSPBuilder {
	b.directives[directive] = append(b.directives[directive], values...)
	return b
}
