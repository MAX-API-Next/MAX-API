package helper

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// CreateFormFileWithContentType creates a multipart file part without copying
// client-controlled names directly into MIME headers.
func CreateFormFileWithContentType(writer *multipart.Writer, fieldName, fileName, contentType string) (io.Writer, error) {
	if writer == nil {
		return nil, errors.New("multipart writer is nil")
	}
	disposition := mime.FormatMediaType("form-data", map[string]string{
		"name":     fieldName,
		"filename": fileName,
	})
	if disposition == "" {
		return nil, errors.New("invalid multipart file name")
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", disposition)
	header.Set("Content-Type", safeMultipartContentType(contentType))
	return writer.CreatePart(header)
}

func safeMultipartContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || mediaType == "" {
		return "application/octet-stream"
	}
	return mediaType
}
