package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadApi(t *testing.T) {
	cfg, err := initConfig()
	require.NoError(t, err)

	if cfg.PrivateKey == "" || cfg.Proof == "" {
		t.SkipNow()
	}

	handlers, err := initHandlers(cfg)
	require.NoError(t, err)

	router := newRouter()
	router.post("/api/v1/upload", handlers.Upload)

	server := httptest.NewServer(router.r)

	f, err := os.Open(filepath.Join("testdata", "test.txt"))
	require.NoError(t, err)
	defer require.NoError(t, f.Close())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", filepath.Base(f.Name()))
	_, _ = io.Copy(part, f)
	_ = writer.Close()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/upload", server.URL), body)
	req.Header.Add("Content-Type", writer.FormDataContentType())
	require.NoError(t, err)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	out, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	var r UploadResponse

	err = json.Unmarshal(out, &r)
	require.NoError(t, err)

	require.Equal(t, "bafkreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku", r.Root)
	require.Equal(t, "bagbaierakdtubdzo53sy6crqkmmwdhomjse3vj5yijkvbopbwt66zqbangpa", r.Shard)
}
