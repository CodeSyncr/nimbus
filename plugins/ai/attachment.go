/*
|--------------------------------------------------------------------------
| AI SDK — Attachments
|--------------------------------------------------------------------------
|
| Images and documents (PDFs) attached to a user turn. A reference is a
| local path, a data: URI or an http(s) URL; providers that cannot fetch
| URLs themselves get the bytes.
|
|   ai.Generate(ctx, "What does this invoice total?", ai.WithFiles("invoice.pdf"))
|   agent.Prompt(ctx, "Describe it", ai.WithImages([]string{"data:image/png;base64,…"}))
|
*/

package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
)

// MaxAttachmentBytes caps one attachment (providers reject larger ones).
const MaxAttachmentBytes = 32 << 20

// Attachment is a loaded image or document.
type Attachment struct {
	MediaType string // e.g. image/png, application/pdf
	Data      []byte
	Name      string // file name, when known
}

// Base64 returns the data base64-encoded.
func (a Attachment) Base64() string { return base64.StdEncoding.EncodeToString(a.Data) }

// IsImage reports whether the attachment is an image.
func (a Attachment) IsImage() bool { return strings.HasPrefix(a.MediaType, "image/") }

// IsPDF reports whether the attachment is a PDF.
func (a Attachment) IsPDF() bool { return a.MediaType == "application/pdf" }

// DataURI renders the attachment as a data: URI.
func (a Attachment) DataURI() string { return "data:" + a.MediaType + ";base64," + a.Base64() }

var attachmentHTTP = &http.Client{Timeout: 30 * time.Second}

// LoadAttachment reads a local path, data: URI or http(s) URL.
func LoadAttachment(ctx context.Context, ref string) (Attachment, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "data:"):
		return parseDataURI(ref)
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		return fetchAttachment(ctx, ref)
	case ref == "":
		return Attachment{}, fmt.Errorf("ai: empty attachment reference")
	}
	path := ref
	if _, err := os.Stat(path); err != nil {
		// Paths given as "/public/x.png" are often relative to the app root.
		if alt := strings.TrimPrefix(path, "/"); alt != path {
			if _, err2 := os.Stat(alt); err2 == nil {
				path = alt
			}
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("ai: attachment %q: %w", ref, err)
	}
	if info.Size() > MaxAttachmentBytes {
		return Attachment{}, fmt.Errorf("ai: attachment %q is larger than %d MB", ref, MaxAttachmentBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("ai: attachment %q: %w", ref, err)
	}
	return Attachment{MediaType: mediaTypeOf(filepath.Base(path), "", data), Data: data, Name: filepath.Base(path)}, nil
}

func parseDataURI(ref string) (Attachment, error) {
	head, payload, ok := strings.Cut(strings.TrimPrefix(ref, "data:"), ",")
	if !ok {
		return Attachment{}, fmt.Errorf("ai: malformed data URI")
	}
	mediaType := strings.Split(head, ";")[0]
	var data []byte
	if strings.Contains(head, ";base64") {
		var err error
		if data, err = base64.StdEncoding.DecodeString(payload); err != nil {
			if data, err = base64.RawStdEncoding.DecodeString(payload); err != nil {
				return Attachment{}, fmt.Errorf("ai: data URI is not valid base64: %w", err)
			}
		}
	} else {
		data = []byte(payload)
	}
	if len(data) > MaxAttachmentBytes {
		return Attachment{}, fmt.Errorf("ai: data URI is larger than %d MB", MaxAttachmentBytes>>20)
	}
	return Attachment{MediaType: mediaTypeOf("", mediaType, data), Data: data}, nil
}

func fetchAttachment(ctx context.Context, url string) (Attachment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Attachment{}, err
	}
	resp, err := attachmentHTTP.Do(req)
	if err != nil {
		return Attachment{}, fmt.Errorf("ai: fetch attachment %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Attachment{}, fmt.Errorf("ai: fetch attachment %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxAttachmentBytes+1))
	if err != nil {
		return Attachment{}, fmt.Errorf("ai: fetch attachment %s: %w", url, err)
	}
	if len(data) > MaxAttachmentBytes {
		return Attachment{}, fmt.Errorf("ai: attachment %s is larger than %d MB", url, MaxAttachmentBytes>>20)
	}
	name := filepath.Base(strings.SplitN(url, "?", 2)[0])
	return Attachment{MediaType: mediaTypeOf(name, resp.Header.Get("Content-Type"), data), Data: data, Name: name}, nil
}

// mediaTypeOf picks a media type from a declared one, the file extension,
// or the content itself.
func mediaTypeOf(name, declared string, data []byte) string {
	if mt, _, err := mime.ParseMediaType(declared); err == nil && mt != "" && mt != "application/octet-stream" {
		return mt
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".md":
		return "text/plain"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".webm":
		return "audio/webm"
	case ".flac":
		return "audio/flac"
	}
	mt := http.DetectContentType(data)
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = mt[:i]
	}
	return mt
}

// loadAttachments loads every reference, logging and skipping the ones
// that cannot be read rather than failing the whole request.
func loadAttachments(refs []string) []Attachment {
	out := make([]Attachment, 0, len(refs))
	for _, ref := range refs {
		a, err := LoadAttachment(context.Background(), ref)
		if err != nil {
			log.Printf("[ai] skipping attachment: %v", err)
			continue
		}
		out = append(out, a)
	}
	return out
}

// MaxDocumentTextChars caps the text extracted from one document for
// providers that cannot read PDFs themselves.
const MaxDocumentTextChars = 200_000

// DocumentText extracts the text of a PDF or plain-text attachment.
func DocumentText(a Attachment) (string, error) {
	var text string
	switch {
	case a.IsPDF():
		r, err := pdf.NewReader(bytes.NewReader(a.Data), int64(len(a.Data)))
		if err != nil {
			return "", fmt.Errorf("ai: read PDF %s: %w", a.Name, err)
		}
		plain, err := r.GetPlainText()
		if err != nil {
			return "", fmt.Errorf("ai: extract PDF text %s: %w", a.Name, err)
		}
		b, err := io.ReadAll(plain)
		if err != nil {
			return "", err
		}
		text = string(b)
	case strings.HasPrefix(a.MediaType, "text/"), a.MediaType == "application/json":
		text = string(a.Data)
	default:
		return "", fmt.Errorf("ai: cannot read text from %s", a.MediaType)
	}
	text = strings.TrimSpace(text)
	if len(text) > MaxDocumentTextChars {
		text = text[:MaxDocumentTextChars] + "\n…(truncated)"
	}
	return text, nil
}

// contentWithDocuments appends the text of the message's non-image files
// to its content, for providers that cannot take documents natively.
func contentWithDocuments(m Message) string {
	if len(m.Files) == 0 {
		return m.Content
	}
	var b strings.Builder
	b.WriteString(m.Content)
	for _, a := range loadAttachments(m.Files) {
		if a.IsImage() {
			continue
		}
		text, err := DocumentText(a)
		if err != nil {
			log.Printf("[ai] skipping file: %v", err)
			continue
		}
		name := a.Name
		if name == "" {
			name = "document"
		}
		b.WriteString("\n\n<file name=\"" + name + "\">\n" + text + "\n</file>")
	}
	return b.String()
}

// imageRefs returns a message's images plus any images given as files.
func imageRefs(m Message) []string {
	refs := append([]string(nil), m.Images...)
	for _, f := range m.Files {
		if a, err := LoadAttachment(context.Background(), f); err == nil && a.IsImage() {
			refs = append(refs, a.DataURI())
		}
	}
	return refs
}
