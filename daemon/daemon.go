// Package daemon runs the memory-md socket server.
//
// Startup sequence (per plan):
//  1. Validate MEMORY_MD_DIR.
//  2. Derive cache dir.
//  3. Open SQLite.
//  4. Remove stale channel.sock if present.
//  5. Write sidecar script; spawn uv; wait for sidecar.sock.
//  6. Walk and sync MEMORY_MD_DIR.
//  7. Bind channel.sock.
//  8. Start fsnotify watcher.
//  9. Serve until SIGTERM/SIGINT.
//
// 10. Shutdown: remove socket, kill sidecar, close SQLite.
package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"memory-md/internal/db"
	"memory-md/internal/engine"
	"memory-md/internal/pathenc"
	"memory-md/internal/watcher"
	"memory-md/sidecar"
)

// Run is the entry point for `memory-md start-daemon`.
func Run(memDir string) error {
	// 1. Validate memDir.
	if fi, err := os.Stat(memDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("MEMORY_MD_DIR is not a directory: %s", memDir)
	}

	// 2. Derive cache dir.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}
	baseDir := filepath.Join(home, ".cache", "memory-md")
	cacheDir := filepath.Join(baseDir, pathenc.Encode(memDir))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("cannot create cache dir: %w", err)
	}
	// Write a human-readable breadcrumb so the hashed dir is identifiable.
	dirFile := filepath.Join(cacheDir, "dir")
	_ = os.WriteFile(dirFile, []byte(memDir+"\n"), 0644)

	// 3. Open SQLite.
	sqlitePath := filepath.Join(cacheDir, "cache.sqlite")
	database, err := db.Open(sqlitePath)
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer database.Close()

	sockPath := filepath.Join(cacheDir, "channel.sock")
	sidecarSockPath := filepath.Join(cacheDir, "sidecar.sock")

	// 4. Remove stale channel.sock.
	os.Remove(sockPath)

	// 5. Sidecar lifecycle.
	var sidecarCmd *exec.Cmd
	if uvPath, err := exec.LookPath("uv"); err == nil {
		scriptPath := filepath.Join(baseDir, "embed.py")
		// Write script if absent or changed.
		if needsWrite(scriptPath, sidecar.Script) {
			if err := os.WriteFile(scriptPath, sidecar.Script, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "memory-md: cannot write sidecar script: %v\n", err)
			}
		}
		sidecarEnv := append(os.Environ(),
			"MEMORY_MD_SIDECAR_SOCK="+sidecarSockPath,
		)
		if model := os.Getenv("MEMORY_MD_EMBED_MODEL"); model != "" {
			sidecarEnv = append(sidecarEnv, "MEMORY_MD_EMBED_MODEL="+model)
		}
		sidecarCmd = exec.Command(uvPath, "run", scriptPath)
		sidecarCmd.Env = sidecarEnv
		sidecarCmd.Stdout = os.Stderr
		sidecarCmd.Stderr = os.Stderr
		if err := sidecarCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "memory-md: cannot start sidecar: %v (FTS5-only mode)\n", err)
			sidecarCmd = nil
		} else {
			// Wait up to 30 s for sidecar.sock to appear.
			deadline := time.Now().Add(30 * time.Second)
			ready := false
			for time.Now().Before(deadline) {
				if _, err := os.Stat(sidecarSockPath); err == nil {
					ready = true
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if !ready {
				fmt.Fprintln(os.Stderr, "memory-md: sidecar did not start within 30s (FTS5-only mode)")
				sidecarCmd.Process.Kill()
				sidecarCmd = nil
			}
		}
	}

	// Build engine.
	var sidecarSock string
	if sidecarCmd != nil {
		sidecarSock = sidecarSockPath
	}
	eng := engine.New(database, memDir, sidecarSock)

	// 6. Walk and sync.
	if err := eng.SyncDir(); err != nil {
		fmt.Fprintf(os.Stderr, "memory-md: sync error: %v\n", err)
	}

	// 7. Bind channel.sock.
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("cannot bind socket: %w", err)
	}

	// 8. Start watcher.
	w, err := watcher.New(eng, memDir)
	if err != nil {
		listener.Close()
		return fmt.Errorf("cannot start watcher: %w", err)
	}
	go w.Run()

	// 9. Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		listener.Close()
	}()

	fmt.Fprintf(os.Stderr, "memory-md daemon ready (%s)\n", sockPath)

	// Serve.
	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go serve(conn, eng)
	}

	// 10. Shutdown.
	w.Close()
	os.Remove(sockPath)
	if sidecarCmd != nil {
		sidecarCmd.Process.Kill()
		sidecarCmd.Wait()
	}
	return nil
}

// ── Request / response types ──────────────────────────────────────────────────

type request struct {
	Cmd     string `json:"Cmd"`
	Path    string `json:"Path,omitempty"`
	Name    string `json:"Name,omitempty"`
	Heading string `json:"Heading,omitempty"`
	Content string `json:"Content,omitempty"`
	Query   string `json:"Query,omitempty"`
	Top     int    `json:"Top,omitempty"`
}

type errResponse struct {
	Ok    bool   `json:"Ok"`
	Error string `json:"Error"`
}

type okResponse struct {
	Ok bool `json:"Ok"`
}

type listResponse struct {
	Ok    bool     `json:"Ok"`
	Items []string `json:"Items"`
}

type pingResponse struct {
	Ok         bool   `json:"Ok"`
	Sidecar    bool   `json:"Sidecar"`
	MemDir     string `json:"MemDir"`
	IsIndexing bool   `json:"IsIndexing"`
}

type getResponse struct {
	Ok      bool   `json:"Ok"`
	Heading string `json:"Heading"`
	Content string `json:"Content"`
}

type searchItem struct {
	Path    string `json:"Path"`
	Heading string `json:"Heading"`
	Content string `json:"Content"`
}

type searchResponse struct {
	Ok      bool         `json:"Ok"`
	Results []searchItem `json:"Results"`
}

// ── Request handler ───────────────────────────────────────────────────────────

func serve(conn net.Conn, eng *engine.Engine) {
	defer conn.Close()

	var req request
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		writeJSON(conn, errResponse{Ok: false, Error: "empty request"})
		return
	}
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		writeJSON(conn, errResponse{Ok: false, Error: "malformed request: " + err.Error()})
		return
	}

	switch req.Cmd {
	case "get":
		res, err := eng.Get(req.Path)
		if err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, getResponse{Ok: true, Heading: res.Heading, Content: res.Content})

	case "search":
		top := req.Top
		if top <= 0 {
			top = 5
		}
		results, err := eng.Search(req.Query, top)
		if err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		items := make([]searchItem, len(results))
		for i, r := range results {
			items[i] = searchItem{Path: r.Path, Heading: r.Heading, Content: r.Content}
		}
		writeJSON(conn, searchResponse{Ok: true, Results: items})

	case "new":
		if err := eng.New(req.Path, req.Heading, req.Content); err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, okResponse{Ok: true})

	case "update":
		if err := eng.Update(req.Path, req.Content); err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, okResponse{Ok: true})

	case "delete":
		if err := eng.Delete(req.Path); err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, okResponse{Ok: true})

	case "create-file":
		if err := eng.CreateFile(req.Name); err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, okResponse{Ok: true})

	case "delete-file":
		if err := eng.DeleteFile(req.Name); err != nil {
			writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
			return
		}
		writeJSON(conn, okResponse{Ok: true})

	case "list":
		if req.Name == "" {
			names, err := eng.ListFiles()
			if err != nil {
				writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
				return
			}
			if names == nil {
				names = []string{}
			}
			writeJSON(conn, listResponse{Ok: true, Items: names})
		} else {
			paths, err := eng.ListSections(req.Name)
			if err != nil {
				writeJSON(conn, errResponse{Ok: false, Error: err.Error()})
				return
			}
			if paths == nil {
				paths = []string{}
			}
			writeJSON(conn, listResponse{Ok: true, Items: paths})
		}

	case "ping":
		writeJSON(conn, pingResponse{Ok: true, Sidecar: eng.SidecarActive(), MemDir: eng.MemDir(), IsIndexing: eng.IsIndexing()})

	default:
		writeJSON(conn, errResponse{Ok: false, Error: "unknown command: " + req.Cmd})
	}
}

func writeJSON(conn net.Conn, v any) {
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	conn.Write(data)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// needsWrite returns true if the file at path is absent or its contents differ from data.
func needsWrite(path string, data []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return !bytes.Equal(existing, data)
}
