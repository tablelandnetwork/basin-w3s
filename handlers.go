package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"golang.org/x/exp/slog"
)

// Handlers groups a bunch of HTTP handlers.
type Handlers struct {
	uploader *Uploader
	tmpDir   string
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
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	// parse file field
	p, err := reader.NextPart()
	if err != nil && err != io.EOF {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	if p.FormName() != "file" {
		http.Error(rw, "file is expected", http.StatusBadRequest)
		return
	}

	buf := bufio.NewReader(p)
	result, err := h.uploader.Upload(r.Context(), buf)
	if err != nil {
		slog.Error("file upload", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	response := &UploadResponse{
		Root:  result.Root.String(),
		Shard: result.Shard.String(),
	}

	bytes, err := json.Marshal(response)
	if err != nil {
		slog.Error("json marshaling", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = rw.Write(bytes)
}

func initHandlers(cfg *config) (*Handlers, error) {
	proof, err := hex.DecodeString(cfg.Proof)
	if err != nil {
		return nil, err
	}

	uploader, err := NewUploader(cfg.SpaceID, cfg.PrivateKey, proof, cfg.TmpDir)
	if err != nil {
		return nil, err
	}

	return &Handlers{
		uploader: uploader,
		tmpDir:   cfg.TmpDir,
	}, nil
}
