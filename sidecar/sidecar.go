// Package sidecar embeds the Python embedding script and provides a Unix
// socket client for the sidecar process.
//
// The embedded script (embed.py) is written to the cache directory by the
// daemon at startup, then launched with "uv run <path>".
//
// Client communicates with the running sidecar over sidecar.sock using
// newline-delimited JSON:
//
//	Daemon → Sidecar: {"Texts": ["text1", ...]}\n
//	Sidecar → Daemon: {"Embeddings": [[0.1, ...], ...]}\n
package sidecar

import (
	_ "embed"

	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

// Script holds the raw bytes of embed.py, embedded at compile time.
//
//go:embed embed.py
var Script []byte

// Client is a thin Unix socket client for the embedding sidecar.
type Client struct {
	sockPath string
}

// New returns a Client that will connect to the sidecar at sockPath.
func New(sockPath string) *Client {
	return &Client{sockPath: sockPath}
}

type embedRequest struct {
	Texts []string
}

type embedResponse struct {
	Embeddings [][]float64
	Error      string
}

// Embed sends texts to the sidecar and returns one []float32 per text.
// Returns nil, nil when the sidecar socket is not present (graceful no-op).
func (c *Client) Embed(texts []string) ([][]float32, error) {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		// Sidecar not running — graceful no-op.
		return nil, nil
	}
	defer conn.Close()

	raw, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("sidecar: marshal request: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := conn.Write(raw); err != nil {
		return nil, fmt.Errorf("sidecar: write request: %w", err)
	}

	var resp embedResponse
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("sidecar: read response: %w", err)
		}
		return nil, fmt.Errorf("sidecar: empty response")
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("sidecar: unmarshal response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("sidecar error: %s", resp.Error)
	}

	result := make([][]float32, len(resp.Embeddings))
	for i, vec := range resp.Embeddings {
		f32 := make([]float32, len(vec))
		for j, v := range vec {
			f32[j] = float32(v)
		}
		result[i] = f32
	}
	return result, nil
}

// EmbedOne embeds a single text. Returns nil, nil when the sidecar is absent.
func (c *Client) EmbedOne(text string) ([]float32, error) {
	vecs, err := c.Embed([]string{text})
	if err != nil || vecs == nil {
		return nil, err
	}
	return vecs[0], nil
}
