// memory-md — persistent markdown-backed memory store.
//
// Usage:
//
//	memory-md start-daemon
//	memory-md get <path>
//	memory-md search <query> [--top N]
//	memory-md new <path> [--heading <text>]
//	memory-md update <path>
//	memory-md delete <path>
//	memory-md create-file <name>
//	memory-md delete-file <name>
//	memory-md snapshot
//	memory-md validate-file <name>
//	memory-md version
//
// All subcommands require MEMORY_MD_DIR to be set, except version.
// snapshot and validate-file are client-side only (no daemon needed).
// All other subcommands (except start-daemon) require the daemon to be running.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"memory-md/daemon"
	"memory-md/internal/parser"
	"memory-md/internal/pathenc"
)

// version is set at build time via -ldflags "-X main.version=<ver>".
// Defaults to "dev" for local builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// version and help do not require MEMORY_MD_DIR.
	if cmd == "version" {
		fmt.Println("memory-md", version)
		return
	}
	if cmd == "help" {
		printUsage(os.Stdout)
		return
	}

	memDir := os.Getenv("MEMORY_MD_DIR")
	if memDir == "" {
		fatal("MEMORY_MD_DIR is not set")
	}

	switch cmd {
	case "start-daemon":
		if err := daemon.Run(memDir); err != nil {
			fatal(err.Error())
		}

	case "snapshot":
		runSnapshot(memDir)

	case "validate-file":
		if len(args) == 0 {
			fatal("usage: memory-md validate-file <name>")
		}
		runValidateFile(memDir, args[0])

	case "status":
		runStatus(memDir)

	// Socket-based subcommands.
	case "list":
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "list", "Name": name})
		handleListResponse(resp)

	case "get":
		if len(args) == 0 {
			fatal("usage: memory-md get <path>")
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "get", "Path": args[0]})
		handleGetResponse(resp)

	case "search":
		if len(args) == 0 {
			fatal("usage: memory-md search <query> [--top N]")
		}
		query := args[0]
		top := 5
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--top" {
				fmt.Sscanf(args[i+1], "%d", &top)
			}
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "search", "Query": query, "Top": top})
		handleSearchResponse(resp)

	case "new":
		if len(args) == 0 {
			fatal("usage: memory-md new <path> [--heading <text>]")
		}
		path := args[0]
		heading := ""
		for i := 1; i < len(args)-1; i++ {
			if args[i] == "--heading" {
				heading = args[i+1]
			}
		}
		content := readStdin()
		resp := sendRequest(memDir, map[string]any{
			"Cmd":     "new",
			"Path":    path,
			"Heading": heading,
			"Content": content,
		})
		handleOkResponse(resp)

	case "update":
		if len(args) == 0 {
			fatal("usage: memory-md update <path>")
		}
		content := readStdin()
		resp := sendRequest(memDir, map[string]any{
			"Cmd":     "update",
			"Path":    args[0],
			"Content": content,
		})
		handleOkResponse(resp)

	case "delete":
		if len(args) == 0 {
			fatal("usage: memory-md delete <path>")
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "delete", "Path": args[0]})
		handleOkResponse(resp)

	case "create-file":
		if len(args) == 0 {
			fatal("usage: memory-md create-file <name>")
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "create-file", "Name": args[0]})
		handleOkResponse(resp)

	case "delete-file":
		if len(args) == 0 {
			fatal("usage: memory-md delete-file <name>")
		}
		resp := sendRequest(memDir, map[string]any{"Cmd": "delete-file", "Name": args[0]})
		handleOkResponse(resp)

	default:
		usageAndExit()
	}
}

func handleStatusResponse(resp map[string]any) {
	if ok, _ := resp["Ok"].(bool); !ok {
		errMsg, _ := resp["Error"].(string)
		fatal(errMsg)
	}
	sidecar, _ := resp["Sidecar"].(bool)
	memDir, _ := resp["MemDir"].(string)
	isIndexing, _ := resp["IsIndexing"].(bool)
	fmt.Printf("daemon:  running  (%s)\n", memDir)
	if sidecar {
		fmt.Println("sidecar: active   (vector search enabled)")
	} else {
		fmt.Println("sidecar: inactive (FTS5-only mode)")
	}
	if isIndexing {
		fmt.Println("indexing: active")
	} else {
		fmt.Println("indexing: idle")
	}
}

func handleListResponse(resp map[string]any) {
	if ok, _ := resp["Ok"].(bool); !ok {
		errMsg, _ := resp["Error"].(string)
		fatal(errMsg)
	}
	rawItems, _ := resp["Items"].([]any)
	for _, item := range rawItems {
		if s, ok := item.(string); ok {
			fmt.Println(s)
		}
	}
}

func runStatus(memDir string) {
	sock := sockPath(memDir)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		fmt.Println("daemon:  not running")
		return
	}
	conn.Close()
	resp := sendRequest(memDir, map[string]any{"Cmd": "ping"})
	handleStatusResponse(resp)
}

// ── Client-side subcommands ───────────────────────────────────────────────────

func runSnapshot(memDir string) {
	ts := time.Now().UTC().Format("20060102-150405")
	snapDir := filepath.Join(memDir, "snapshot-"+ts)

	if _, err := os.Stat(snapDir); err == nil {
		fatal("snapshot directory already exists: snapshot-" + ts)
	}
	if err := os.Mkdir(snapDir, 0755); err != nil {
		fatal("cannot create snapshot directory: " + err.Error())
	}

	entries, err := os.ReadDir(memDir)
	if err != nil {
		fatal("cannot read MEMORY_MD_DIR: " + err.Error())
	}

	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		src := filepath.Join(memDir, de.Name())
		dst := filepath.Join(snapDir, de.Name())
		if err := copyFile(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "memory-md snapshot: copy %s: %v\n", de.Name(), err)
		}
	}

	fmt.Println(snapDir)
}

func runValidateFile(memDir, name string) {
	if err := validateName(name); err != nil {
		fatal(err.Error())
	}
	filePath := filepath.Join(memDir, name+".md")
	src, err := os.ReadFile(filePath)
	if err != nil {
		fatal("cannot read file: " + err.Error())
	}

	result := parser.Parse(filePath, src)
	issues := parser.ValidateFile(result)

	if len(issues) == 0 {
		fmt.Printf("%s: ok\n", name)
		return
	}
	for _, issue := range issues {
		fmt.Printf("%s:%d: %s\n", name, issue.Line, issue.Message)
	}
	os.Exit(1)
}

// ── Socket client ─────────────────────────────────────────────────────────────

func sockPath(memDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal("cannot determine home dir: " + err.Error())
	}
	return filepath.Join(home, ".cache", "memory-md", pathenc.Encode(memDir), "channel.sock")
}

func sendRequest(memDir string, req map[string]any) map[string]any {
	sock := sockPath(memDir)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		fatal("cannot connect to daemon (is it running?): " + err.Error())
	}
	defer conn.Close()

	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		fatal("cannot send request: " + err.Error())
	}

	var resp map[string]any
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		fatal("no response from daemon")
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		fatal("malformed response from daemon: " + err.Error())
	}
	return resp
}

func handleOkResponse(resp map[string]any) {
	if ok, _ := resp["Ok"].(bool); !ok {
		errMsg, _ := resp["Error"].(string)
		fatal(errMsg)
	}
}

func handleGetResponse(resp map[string]any) {
	if ok, _ := resp["Ok"].(bool); !ok {
		errMsg, _ := resp["Error"].(string)
		fatal(errMsg)
	}
	heading, _ := resp["Heading"].(string)
	content, _ := resp["Content"].(string)
	fmt.Printf("%s\n\n%s\n", heading, content)
}

func handleSearchResponse(resp map[string]any) {
	if ok, _ := resp["Ok"].(bool); !ok {
		errMsg, _ := resp["Error"].(string)
		fatal(errMsg)
	}
	rawResults, _ := resp["Results"].([]any)
	first := true
	for _, r := range rawResults {
		item, _ := r.(map[string]any)
		path, _ := item["Path"].(string)
		heading, _ := item["Heading"].(string)
		content, _ := item["Content"].(string)
		if !first {
			fmt.Println()
		}
		first = false
		fmt.Printf("=== %s ===\n%s\n\n%s\n", path, heading, content)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func readStdin() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal("cannot read stdin: " + err.Error())
	}
	return strings.TrimRight(string(data), "\n")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("name must not contain path separators")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("name must not start with '.'")
	}
	if strings.HasSuffix(name, ".md") {
		return fmt.Errorf("name must not include .md suffix")
	}
	return nil
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "memory-md: "+msg)
	os.Exit(1)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: memory-md <command> [args]

Commands:
  status                   Show whether the daemon is running and sidecar mode
  start-daemon              Start the daemon (foreground)
  list [<name>]             List files, or sections within a file
  get <path>                Exact path lookup
  search <query> [--top N]  Hybrid FTS5 + vector search
  new <path> [--heading T]  Create a new section (body from stdin)
  update <path>             Replace section body (from stdin)
  delete <path>             Delete a section and its children
  create-file <name>        Create a new empty .md file
  delete-file <name>        Delete a .md file and its index data
  snapshot                  Copy all .md files into a timestamped subdirectory
  validate-file <name>      Validate structural rules of a .md file
  version                   Print version and exit
  help                      Show this help and exit`)
}

func usageAndExit() {
	printUsage(os.Stderr)
	os.Exit(1)
}
