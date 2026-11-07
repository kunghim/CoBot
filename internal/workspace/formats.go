package workspace

import (
	"path/filepath"
	"strings"
)

// Format is one kind of file the viewer knows: the extensions that name it and
// the type its bytes are served with. Executable formats stay inert: html and
// the other text kinds go out as plain text.
type Format struct {
	Kind        string
	Extensions  []string
	ContentType func(name string) string // nil means opaque bytes
}

// binaryContentType is what a file of no known type is served as.
const binaryContentType = "application/octet-stream"

// formats lists every kind but directory and blocked, which are never served as
// a file's content. text has no extensions: any file without one of these, whose
// sample reads as text, is text.
var formats = []Format{
	{Kind: "text", ContentType: plainText},
	{Kind: "markdown", Extensions: []string{".md", ".markdown", ".mdown", ".mkd", ".mdx"}, ContentType: plainText},
	{Kind: "html", Extensions: []string{".html", ".htm"}, ContentType: plainText},
	{Kind: "svg", Extensions: []string{".svg"}, ContentType: fixedType("image/svg+xml")},
	{Kind: "image", Extensions: []string{".avif", ".bmp", ".gif", ".ico", ".jpeg", ".jpg", ".png", ".webp"}, ContentType: imageType},
	{Kind: "pdf", Extensions: []string{".pdf"}, ContentType: fixedType("application/pdf")},
	{Kind: "audio", Extensions: []string{".aac", ".flac", ".m4a", ".mp3", ".mp4a", ".oga", ".ogg", ".opus", ".wav"}, ContentType: typeByExtension(map[string]string{
		".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".mp4a": "audio/mp4", ".oga": "audio/ogg", ".opus": "audio/ogg", ".ogg": "audio/ogg",
		".wav": "audio/wav", ".flac": "audio/flac", ".aac": "audio/aac",
	})},
	{Kind: "video", Extensions: []string{".mp4", ".ogv", ".webm"}, ContentType: typeByExtension(map[string]string{
		".mp4": "video/mp4", ".ogv": "video/ogg", ".webm": "video/webm",
	})},
	{Kind: "document", Extensions: []string{".docx"}},
	{Kind: "spreadsheet", Extensions: []string{".xlsx", ".xlsm", ".csv", ".tsv"}},
	{Kind: "presentation", Extensions: []string{".pptx"}},
	{Kind: "notebook", Extensions: []string{".ipynb"}, ContentType: plainText},
	{Kind: "latex", Extensions: []string{".tex"}, ContentType: plainText},
	{Kind: "binary", Extensions: []string{".doc", ".xls", ".ppt", ".rtf", ".pages", ".numbers", ".key", ".odt", ".ods", ".odp", ".tif", ".tiff"}},
}

var kindByExtension = func() map[string]string {
	kinds := map[string]string{}
	for _, format := range formats {
		for _, ext := range format.Extensions {
			kinds[ext] = format.Kind
		}
	}
	return kinds
}()

// ContentType is the type a file of this kind is served with.
func ContentType(kind, name string) string {
	for _, format := range formats {
		if format.Kind == kind && format.ContentType != nil {
			return format.ContentType(name)
		}
	}
	return binaryContentType
}

func plainText(string) string { return "text/plain; charset=utf-8" }

func fixedType(contentType string) func(string) string {
	return func(string) string { return contentType }
}

func imageType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpg" {
		ext = ".jpeg"
	}
	if ext == ".ico" {
		return "image/x-icon"
	}
	return "image/" + strings.TrimPrefix(ext, ".")
}

func typeByExtension(types map[string]string) func(string) string {
	return func(name string) string {
		if contentType, ok := types[strings.ToLower(filepath.Ext(name))]; ok {
			return contentType
		}
		return binaryContentType
	}
}
