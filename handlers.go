package main

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"

	"golang.org/x/exp/slog"
)

// Handlers groups a bunch of HTTP handlers.
type Handlers struct {
	uploader *Uploader
}

// UploadResponse ...
type UploadResponse struct {
	Root  string `json:"root"`
	Shard string `json:"shard"`
}

// Health is a health checker.
func (h *Handlers) Health(rw http.ResponseWriter, _ *http.Request) {
	rw.WriteHeader(http.StatusOK)
}

// Upload handles POST /api/v1/upload.
func (h *Handlers) Upload(rw http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("file")
	if err != nil {
		slog.Error("form file", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("close file", err)
		}
	}()

	result, err := h.uploader.Upload(r.Context(), f)
	if err != nil {
		slog.Error("file upload", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	response := &UploadResponse{
		Root:  result.Root.String(),
		Shard: result.Shard.String(),
	}

	bytes, _ := json.Marshal(response)
	_, _ = rw.Write(bytes)
}

func initHandlers(cfg *config) (*Handlers, error) {
	proof, err := hex.DecodeString(cfg.Proof)
	if err != nil {
		return nil, err
	}

	uploader, err := NewUploader(cfg.PrivateKey, proof, os.TempDir())
	if err != nil {
		return nil, err
	}

	return &Handlers{
		uploader: uploader,
	}, nil
}
