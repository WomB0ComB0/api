package model

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"api/media/internal/pkg/apperr"
)

// FileType represents a canonical media type recognized from authoritative leading bytes
// (mirrors @resq-systems/security/controls/upload for OWASP WSTG-BUSL-08/09 compliance).
type FileType string

const (
	FileTypePNG   FileType = "png"
	FileTypeJPEG  FileType = "jpeg"
	FileTypeGIF   FileType = "gif"
	FileTypeWebP  FileType = "webp"
	FileTypeAVIF  FileType = "avif"
	FileTypeICO   FileType = "ico"
	FileTypePDF   FileType = "pdf"
	FileTypeMP4   FileType = "mp4"
	FileTypeWebM  FileType = "webm"
	FileTypeMP3   FileType = "mp3"
	FileTypeWAV   FileType = "wav"
	FileTypeOGG   FileType = "ogg"
	FileTypeZIP   FileType = "zip"
	FileTypeWOFF2 FileType = "woff2"
	FileTypeSVG   FileType = "svg"
	FileTypeText  FileType = "text"
)

const MaxSVGBytes int64 = 2 << 20 // 2 MiB ceiling for SVG active-content scanning

type typeSpec struct {
	extensions    []string
	canonicalMIME string
	allowedMIMEs  []string
}

var fileTypeSpecs = map[FileType]typeSpec{
	FileTypePNG:   {extensions: []string{".png"}, canonicalMIME: "image/png", allowedMIMEs: []string{"image/png"}},
	FileTypeJPEG:  {extensions: []string{".jpg", ".jpeg"}, canonicalMIME: "image/jpeg", allowedMIMEs: []string{"image/jpeg", "image/jpg"}},
	FileTypeGIF:   {extensions: []string{".gif"}, canonicalMIME: "image/gif", allowedMIMEs: []string{"image/gif"}},
	FileTypeWebP:  {extensions: []string{".webp"}, canonicalMIME: "image/webp", allowedMIMEs: []string{"image/webp"}},
	FileTypeAVIF:  {extensions: []string{".avif"}, canonicalMIME: "image/avif", allowedMIMEs: []string{"image/avif"}},
	FileTypeICO:   {extensions: []string{".ico"}, canonicalMIME: "image/x-icon", allowedMIMEs: []string{"image/x-icon", "image/vnd.microsoft.icon"}},
	FileTypePDF:   {extensions: []string{".pdf"}, canonicalMIME: "application/pdf", allowedMIMEs: []string{"application/pdf"}},
	FileTypeMP4:   {extensions: []string{".mp4"}, canonicalMIME: "video/mp4", allowedMIMEs: []string{"video/mp4", "audio/mp4"}},
	FileTypeWebM:  {extensions: []string{".webm"}, canonicalMIME: "video/webm", allowedMIMEs: []string{"video/webm", "audio/webm"}},
	FileTypeMP3:   {extensions: []string{".mp3"}, canonicalMIME: "audio/mpeg", allowedMIMEs: []string{"audio/mpeg", "audio/mp3"}},
	FileTypeWAV:   {extensions: []string{".wav"}, canonicalMIME: "audio/wav", allowedMIMEs: []string{"audio/wav", "audio/x-wav", "audio/wave"}},
	FileTypeOGG:   {extensions: []string{".ogg"}, canonicalMIME: "audio/ogg", allowedMIMEs: []string{"audio/ogg", "video/ogg", "application/ogg"}},
	FileTypeZIP:   {extensions: []string{".zip"}, canonicalMIME: "application/zip", allowedMIMEs: []string{"application/zip", "application/x-zip-compressed"}},
	FileTypeWOFF2: {extensions: []string{".woff2"}, canonicalMIME: "font/woff2", allowedMIMEs: []string{"font/woff2"}},
	FileTypeSVG:   {extensions: []string{".svg"}, canonicalMIME: "image/svg+xml", allowedMIMEs: []string{"image/svg+xml"}},
	FileTypeText:  {extensions: []string{".txt", ".csv", ".json", ".md"}, canonicalMIME: "text/plain; charset=utf-8", allowedMIMEs: []string{"text/plain", "text/csv", "application/json", "text/markdown", "text/x-markdown"}},
}

var extToCanonicalTextMIME = map[string]string{
	".txt":  "text/plain; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".json": "application/json",
	".md":   "text/markdown; charset=utf-8",
}

var (
	svgOpeningRe = regexp.MustCompile(`(?is)^\s*(?:<\?xml[^>]*\?>\s*)?(?:<!DOCTYPE\s+svg[^>]*>\s*)?(?:<!--.*?-->\s*)*<svg\b`)
	svgActiveRe  = regexp.MustCompile(`(?i)<\s*(?:script|foreignobject|iframe|object|embed|applet|meta)\b|<!\s*entity\b|\bon[a-z]{2,32}\s*=|(?:javascript|vbscript)\s*:|data\s*:\s*text/html`)
	htmlPrefixRe = regexp.MustCompile(`(?i)^\s*(?:<!doctype\s+html|<html\b|<script\b|<iframe\b|<\?php\b)`)
)

var (
	avifBrands = [][]byte{[]byte("avif"), []byte("avis"), []byte("mif1"), []byte("msf1")}
	mp4Brands  = [][]byte{
		[]byte("isom"), []byte("iso2"), []byte("iso4"), []byte("iso5"), []byte("iso6"),
		[]byte("mp41"), []byte("mp42"), []byte("mmp4"), []byte("avc1"), []byte("dash"),
		[]byte("M4V "), []byte("M4A "), []byte("M4P "), []byte("M4B "), []byte("qt  "),
		[]byte("3gp4"), []byte("3gp5"), []byte("3g2a"), []byte("MSNV"),
	}
)

// DetectFileSignature identifies a file type strictly from its leading bytes.
func DetectFileSignature(head []byte) (FileType, bool) {
	if len(head) == 0 {
		return "", false
	}

	switch {
	case hasPrefix(head, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return FileTypePNG, true
	case hasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return FileTypeJPEG, true
	case hasPrefix(head, []byte("GIF87a")) || hasPrefix(head, []byte("GIF89a")):
		return FileTypeGIF, true
	case len(head) >= 12 && hasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return FileTypeWebP, true
	case len(head) >= 12 && hasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		return FileTypeWAV, true
	case hasPrefix(head, []byte{0x00, 0x00, 0x01, 0x00}):
		return FileTypeICO, true
	case hasPrefix(head, []byte("%PDF-")):
		return FileTypePDF, true
	case hasPrefix(head, []byte("wOF2")):
		return FileTypeWOFF2, true
	case hasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return FileTypeWebM, true
	case hasPrefix(head, []byte("OggS")):
		return FileTypeOGG, true
	case hasPrefix(head, []byte("ID3")) || isMP3FrameSync(head):
		return FileTypeMP3, true
	case hasPrefix(head, []byte{'P', 'K', 0x03, 0x04}) || hasPrefix(head, []byte{'P', 'K', 0x05, 0x06}):
		return FileTypeZIP, true
	case len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")):
		brand := head[8:12]
		if matchesAnyBrand(brand, avifBrands) {
			return FileTypeAVIF, true
		}
		if matchesAnyBrand(brand, mp4Brands) {
			return FileTypeMP4, true
		}
		return "", false
	}

	// Text/SVG classification: require valid UTF-8 with no non-whitespace control bytes.
	sample := head
	if len(sample) > 512 {
		sample = sample[:512]
	}
	sample = bytes.TrimPrefix(sample, []byte{0xEF, 0xBB, 0xBF}) // Strip optional UTF-8 BOM
	// Avoid false UTF-8 failure if a multi-byte rune was truncated at the 512-byte boundary
	for len(sample) > 0 && !utf8.Valid(sample) && len(sample) > len(head)-4 {
		sample = sample[:len(sample)-1]
	}
	if len(sample) == 0 || !utf8.Valid(sample) {
		return "", false
	}
	for _, b := range sample {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' {
			return "", false
		}
	}

	text := string(sample)
	if htmlPrefixRe.MatchString(text) {
		return "", false
	}
	if svgOpeningRe.MatchString(text) {
		return FileTypeSVG, true
	}
	return FileTypeText, true
}

// ValidateUploadContent enforces 3-way agreement between leading magic bytes,
// filename extension, and declared Content-Type, returning the canonical MIME type.
func ValidateUploadContent(filename Filename, declaredContentType string, head []byte) (string, error) {
	if len(head) == 0 {
		return "", apperr.Validation("uploaded file is empty")
	}

	detected, ok := DetectFileSignature(head)
	if !ok {
		return "", apperr.Validation("file content signature is unrecognized or disallowed")
	}

	spec := fileTypeSpecs[detected]
	if !containsString(spec.extensions, filename.Ext) {
		return "", apperr.Validation(
			fmt.Sprintf("file content (%s) does not match extension %q", detected, filename.Ext),
		)
	}

	// For JSON uploads, also ensure the leading non-whitespace byte is a valid JSON value opener.
	if filename.Ext == ".json" {
		trimmed := bytes.TrimSpace(bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}))
		if len(trimmed) == 0 || !bytes.ContainsRune([]byte{'{', '[', '"', '-', 't', 'f', 'n', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}, rune(trimmed[0])) {
			return "", apperr.Validation("file content does not start with valid JSON")
		}
	}

	declared := normalizeMIME(declaredContentType)
	if declared != "" && declared != "application/octet-stream" {
		if !containsString(spec.allowedMIMEs, declared) {
			return "", apperr.Validation(
				fmt.Sprintf("declared content_type %q does not match detected file type (%s)", declared, detected),
			)
		}
	}

	if detected == FileTypeSVG {
		if err := ValidateSVGContent(head); err != nil {
			return "", err
		}
	}

	if detected == FileTypeText {
		if canonical, ok := extToCanonicalTextMIME[filename.Ext]; ok {
			return canonical, nil
		}
	}
	return spec.canonicalMIME, nil
}

// ValidateSVGContent blocks active XSS/XXE constructs in uploaded SVG documents.
func ValidateSVGContent(data []byte) error {
	if !utf8.Valid(data) {
		return apperr.Validation("SVG file must be valid UTF-8")
	}
	if svgActiveRe.Match(data) {
		return apperr.Validation("SVG contains disallowed active content (scripts, event handlers, foreignObject, or XML entities)")
	}
	return nil
}

// ValidatePresignContentType enforces an explicit extension <-> MIME allowlist
// before issuing a presigned R2 PUT URL, returning the canonical MIME to sign.
func ValidatePresignContentType(filename Filename, declaredContentType string) (string, error) {
	declared := normalizeMIME(declaredContentType)

	for ft, spec := range fileTypeSpecs {
		if !containsString(spec.extensions, filename.Ext) {
			continue
		}
		if declared == "" {
			if ft == FileTypeText {
				return extToCanonicalTextMIME[filename.Ext], nil
			}
			return spec.canonicalMIME, nil
		}
		if !containsString(spec.allowedMIMEs, declared) {
			return "", apperr.Validation(
				fmt.Sprintf("content_type %q is not allowed for extension %q", declared, filename.Ext),
			)
		}
		return declared, nil
	}
	return "", apperr.Validation(fmt.Sprintf("file extension %q is not allowed", filename.Ext))
}

func normalizeMIME(raw string) string {
	if raw == "" {
		return ""
	}
	if idx := strings.IndexByte(raw, ';'); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func hasPrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && bytes.Equal(b[:len(prefix)], prefix)
}

func isMP3FrameSync(head []byte) bool {
	return len(head) >= 2 && head[0] == 0xFF && (head[1]&0xE0) == 0xE0 && (head[1]&0x06) != 0x00
}

func matchesAnyBrand(brand []byte, candidates [][]byte) bool {
	for _, c := range candidates {
		if bytes.Equal(brand, c) {
			return true
		}
	}
	return false
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}
