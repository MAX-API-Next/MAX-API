package helper

import (
	"bytes"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateFormFileWithContentTypeEscapesFilename(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := CreateFormFileWithContentType(writer, "file", `quote".png`, "image/png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary())
	form, err := reader.ReadForm(1024)
	require.NoError(t, err)
	defer form.RemoveAll()
	require.Len(t, form.File["file"], 1)
	assert.Equal(t, `quote".png`, form.File["file"][0].Filename)
}

func TestCreateFormFileWithContentTypeEncodesHeaderInjectionCharacters(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := CreateFormFileWithContentType(writer, "file", "image.png\r\nX-Injected: true", "image/png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary())
	parsedPart, err := reader.NextPart()
	require.NoError(t, err)
	assert.Empty(t, parsedPart.Header.Get("X-Injected"))
	assert.False(t, strings.Contains(parsedPart.Header.Get("Content-Disposition"), "\r\n"))
}
