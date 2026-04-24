# memory-md

A persistent, markdown-backed memory store for developers and AI agents. Markdown files are the **source of truth** — human-readable, git-committable, and editable by any text editor. A local daemon indexes them into SQLite for fast retrieval via full-text search (FTS5) and optional vector search.

---

## Features

- **Markdown as source of truth** — every section lives in a plain `.md` file; the SQLite index is always rebuildable.
- **Exact path lookup** — retrieve any section by its hierarchical path (e.g. `auth/api-keys/rotation-policy`).
- **Hybrid search** — FTS5 full-text search combined with vector embeddings fused via Reciprocal Rank Fusion (RRF). Vector search is opt-in and requires Apple Silicon.
- **Daemon architecture** — a lightweight foreground daemon watches files and serves all queries over a Unix socket.
- **Atomic writes** — every file mutation uses a temp-file-then-rename strategy, preventing corruption on interrupted writes.
- **Eventual consistency** — write commands mutate only the markdown file; the daemon's fsnotify watcher (500 ms debounce) is the single write path into the index.
- **Self-contained binary** — SQLite, FTS5, and sqlite-vec are compiled in via CGo. No system libraries required beyond `libSystem.dylib` on macOS.
- **Optional vector search** — if `uv` is in `PATH`, the daemon automatically spawns a Python sidecar (`mlx-embeddings`) that provides 384-dimensional embeddings on Apple Silicon. Falls back to FTS5-only on any other hardware or when `uv` is absent.

---

## Requirements

### Runtime

| Dependency | Required | Purpose |
|---|---|---|
| `MEMORY_MD_DIR` env var | Yes | Path to the markdown memory directory |
| `uv` | Optional | Manages the Python embedding sidecar for vector search |

> **Vector search** requires macOS on Apple Silicon (M1 or later). The Go binary itself is cross-platform.

### Build

| Dependency | Purpose |
|---|---|
| Go ≥ 1.26 | Compiles the binary |
| `zig` (≥ 0.15) | C compiler for CGo — replaces Xcode/gcc/clang |

`mise` is the recommended dev tool manager. It pins exact versions and sets all required env vars automatically.

---

## Installation

### Using mise (recommended)

```sh
git clone https://github.com/yourorg/memory-md
cd memory-md
mise install        # installs go, zig, uv as declared in mise.toml
mise run build      # produces ./memory-md binary
mise run install    # installs memory-md to $HOME/.local/bin
```

### Manual build

```sh
CGO_ENABLED=1 CC="zig cc" go build -tags sqlite_fts5 -o memory-md .
```

> The `-tags sqlite_fts5` flag is required to include FTS5 support in the bundled SQLite amalgamation.

### Manual install to PATH

```sh
cp memory-md /usr/local/bin/
```

---

## Quick start

```sh
# 1. Point memory-md at your notes directory
export MEMORY_MD_DIR=~/notes
mkdir -p "$MEMORY_MD_DIR"

# 2. Start the daemon (runs in foreground; use a separate terminal, tmux, etc.)
memory-md start-daemon

# 3. Create a file and add sections
memory-md create-file auth "Authentication" "Covers auth-related decisions."
echo "Keys are hashed with bcrypt before storage." | memory-md new auth/api-keys --heading "API Keys"
echo "Keys rotate every 90 days."                  | memory-md new auth/api-keys/rotation-policy --heading "Rotation Policy"

# 4. Retrieve a section
memory-md get auth/api-keys

# 5. Search
memory-md search "key rotation" --top 3
```

---

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `MEMORY_MD_DIR` | Yes | — | Path to the markdown memory directory. All subcommands require this except `version`. |
| `MEMORY_MD_EMBED_MODEL` | No | `mlx-community/bge-small-en-v1.5-8bit` | mlx-embeddings model used by the sidecar (Apple Silicon only). |

---

## Commands

```
memory-md <command> [args]
```

| Command | Daemon needed | Description |
|---|---|---|
| `status` | No | Show whether the daemon is running, which search mode is active, and whether indexing is in progress |
| `start-daemon` | — | Start the daemon in the foreground |
| `list [<name>]` | Yes | List all files, or all section paths within a named file |
| `get <path>` | Yes | Exact path lookup |
| `search <query> [--top N]` | Yes | Hybrid FTS5 + vector search (default top-5) |
| `new <path> [--heading T]` | Yes | Create a new section (body read from stdin) |
| `update <path>` | Yes | Replace a section's body (from stdin); child sections are preserved |
| `delete <path>` | Yes | Delete a section and all its children |
| `create-file <name> <title> [description]` | Yes | Create a new `.md` file with a `#` title and optional description |
| `delete-file <name>` | Yes | Delete a `.md` file and all its index data |
| `snapshot` | No | Copy all `.md` files into a timestamped subdirectory |
| `validate-file <name>` | No | Check structural rules of a `.md` file |
| `version` | No | Print version and exit |
| `help` | No | Show usage and exit |

### `status`

Prints daemon health, sidecar mode, and live indexing state. Can be called without the daemon running (reports `not running` in that case).

**Example output (daemon running, idle):**
```
daemon:  running  (/home/user/notes)
sidecar: active   (vector search enabled)
indexing: idle
```

**Example output (daemon indexing a file change):**
```
daemon:  running  (/home/user/notes)
sidecar: active   (vector search enabled)
indexing: active
```

The `indexing: active` state appears while the daemon is processing a file-system event — typically sub-second for small files, a few seconds when the sidecar is generating embeddings for a large batch. Poll `status` to confirm the index has caught up after bulk writes.

---

### `start-daemon`

Runs in the foreground. Use systemd, launchd, tmux, or a process supervisor to manage the lifecycle. The daemon creates a Unix socket at `~/.cache/memory-md/<hash>/channel.sock` and removes it on exit.

If `uv` is found in `PATH`, the daemon writes the embedded Python sidecar to `<cache-dir>/embed.py` and spawns it. It waits up to 30 seconds for the sidecar to become ready. If the sidecar does not start (unsupported hardware, `uv` absent, etc.), the daemon continues in FTS5-only mode.

### `list [<name>]`

```sh
# List all indexed files
memory-md list

# List all sections within a file (ordered by position in file)
memory-md list auth
```

**Output:**
```
# memory-md list
auth
infra

# memory-md list auth
auth/api-keys
auth/api-keys/rotation-policy
auth/oauth
```

Sections are returned in document order (position in the file), not alphabetically.

### `get <path>`

```sh
memory-md get auth/api-keys
```

Exact path lookup. Exits non-zero if the path does not exist.

**Output:**
```
API Keys

Keys are hashed with bcrypt before storage.
```

### `search <query> [--top N]`

```sh
memory-md search "token rotation policy" --top 5
```

With the sidecar running, performs hybrid FTS5 + vector retrieval fused with RRF (K=60). Without the sidecar, falls back to FTS5-only. Default `--top` is 5.

**Output:**
```
=== auth/api-keys ===
API Keys

Keys are hashed with bcrypt before storage.

=== auth/api-keys/rotation-policy ===
Rotation Policy

Keys rotate every 90 days.
```

### `new <path> [--heading <text>]`

Creates a new section. Body content is read from stdin. Fails if the section already exists, if the file does not exist, or (for nested paths) if the parent section does not exist.

After writing the section, `memory-md` immediately validates the target `.md` file. The write is not reverted if validation finds issues; instead, the response includes the validation errors so a human or agent can fix the file manually.

```sh
echo "Body text here." | memory-md new auth/api-keys --heading "API Keys"
echo "Details."        | memory-md new auth/api-keys/rotation-policy   # heading defaults to "rotation-policy"
```

If validation finds a problem, the command prints lines like:

```text
auth:12: duplicate path: auth/api-keys (also at line 8)
```

The heading level is derived from the path depth: 2 segments → `##`, 3 segments → `###`, and so on.

### `update <path>`

Replaces the **immediate body** of an existing section. Child sections are preserved in the file.

Like `new`, `update` validates the target `.md` file immediately after writing. Validation failures do not roll back the edit; the command prints the validation errors so a human or agent can fix the file manually.

```sh
echo "Updated policy." | memory-md update auth/api-keys
```

### `delete <path>`

Removes a section and all its descendants. The splice covers the full subtree.

```sh
memory-md delete auth/api-keys   # removes api-keys and rotation-policy
```

### `create-file <name> <title> [description]` / `delete-file <name>`

`create-file` requires a file name and title, plus an optional description. It writes the new file as a `# <title>` heading followed by the optional description text below it.

Name must not contain `/`, must not start with `.`, and must not include the `.md` suffix.

```sh
memory-md create-file infra "Infrastructure" "Shared infrastructure notes."
memory-md delete-file infra
```

### `snapshot`

Copies all root-level `.md` files into `$MEMORY_MD_DIR/snapshot-<UTC timestamp>/`. Subdirectories (including other snapshots) are ignored by the watcher and are never indexed.

```sh
memory-md snapshot
# prints: /home/user/notes/snapshot-20260410-150405
```

### `validate-file <name>`

Parses a `.md` file and reports structural violations. Exits 0 if clean, exits 1 if issues are found.

```sh
memory-md validate-file auth
```

**Validation rules:**

| # | Rule |
|---|---|
| 1 | At most one `#` heading per file |
| 2 | The `#` heading (if present) must appear before any `##` heading |
| 3 | Heading levels must not skip (no `####` directly under `##`) |
| 4 | No duplicate paths (two sibling headings that slugify identically) |

**Output (clean):**
```
auth: ok
```

**Output (issues):**
```
auth:5: multiple # headings — only one allowed
auth:12: duplicate path: auth/api-keys (also at line 8)
```

---

## Markdown file convention

Each `.md` file groups related sections. The **filename** (without `.md`) is always the first path segment and is never derived from heading text.

### Heading roles

| Heading level | Role |
|---|---|
| `#` | Decorative title — stored as file metadata; ignored for path derivation |
| `##` and deeper | Path segments (slugified heading text) |

Only **ATX-style headings** (`## text`) are recognised. Setext-style headings (`===` / `---` underlines) are treated as ordinary body text.

### Slugification

Heading text is slugified to form path segments: lowercase, spaces → `-`, all non-alphanumeric characters except `-` stripped.

```
"API Keys"              →  api-keys
"Token Refresh Policy"  →  token-refresh-policy
```

### Example file (`auth.md`)

```markdown
# Authentication

Covers all auth-related decisions.

## API Keys

Keys are hashed with bcrypt before storage.

### Rotation Policy

Keys rotate every 90 days.
```

| Path | Content |
|---|---|
| `auth/api-keys` | "Keys are hashed with bcrypt before storage." |
| `auth/api-keys/rotation-policy` | "Keys rotate every 90 days." |

### Duplicate slug handling

If two headings in the same file produce the same path, the **last occurrence wins**. A warning is logged and the earlier entry is overwritten in the index.

---

## Architecture

```
$MEMORY_MD_DIR/          ← source of truth (plain .md files)
  auth.md
  infra.md
  ...

$HOME/.cache/memory-md/
  embed.py               ← Python sidecar script (shared across all projects)
  <hash>/
    dir                  ← plain-text breadcrumb: the absolute MEMORY_MD_DIR path
    cache.sqlite         ← SQLite index (FTS5 + vec0); fully rebuildable
    channel.sock         ← Unix socket; created by daemon, removed on exit
    sidecar.sock         ← embedding sidecar socket (Apple Silicon only)
```

`<hash>` is the first 16 hex characters of `SHA-256(MEMORY_MD_DIR)` — always 16 characters, keeping the socket path well within the 104-byte `sun_path` limit on macOS (and 108-byte limit on Linux). The `dir` breadcrumb file makes the hashed directories identifiable without running the daemon.

### Finding the cache directory for a memory dir

```sh
# Show the cache dir for the current MEMORY_MD_DIR (daemon must be running)
memory-md status

# List all cache dirs and which MEMORY_MD_DIR they belong to
cat ~/.cache/memory-md/*/dir

# Find the cache dir for a specific MEMORY_MD_DIR
grep -rl "/your/notes" ~/.cache/memory-md/*/dir
```

### Daemon

The daemon runs as a **foreground blocking process**. On startup it:

1. Validates `MEMORY_MD_DIR` and creates the cache directory.
2. Opens `cache.sqlite` and applies the schema (FTS5, vec0, triggers).
3. Removes any stale `channel.sock` from a prior crash.
4. If `uv` is in `PATH`, writes `embed.py` and spawns the sidecar subprocess. Waits up to 30 s for `sidecar.sock` to appear.
5. Walks `MEMORY_MD_DIR` (root level only), comparing mtimes against the index. Re-parses and re-embeds only changed files.
6. Removes index entries for files that no longer exist on disk.
7. Binds `channel.sock` and starts the fsnotify watcher (500 ms debounce).
8. Serves newline-delimited JSON requests until SIGTERM/SIGINT.
9. On shutdown: removes `channel.sock`, kills the sidecar, closes SQLite.

### Socket protocol

Each subcommand dials the socket, sends one JSON request, reads one JSON response, and exits. All errors have the shape `{"Ok": false, "Error": "<message>"}`.

### Watcher and eventual consistency

Write commands (`new`, `update`, `delete`, `create-file`, `delete-file`) mutate only the markdown file. The watcher is the **single write path into the index** — the index can never diverge from the file state. There is a brief window (≤ 500 ms debounce) after a write where the index has not yet reflected the change. This is an explicit, accepted trade-off.

### Vector search (Apple Silicon only)

When the sidecar is running:
- Each section is embedded as `path + " " + heading + " " + content` → 384-dimensional float32 vector.
- At search time the query is embedded the same way.
- FTS5 and vec0 each return up to `top × 5` candidate rows; these are fused with RRF (K=60) and the top-K results are returned.

The default model is `mlx-community/bge-small-en-v1.5-8bit` (~35 MB quantized, runs on the Apple Neural Engine).

---

## SQLite schema overview

| Table | Purpose |
|---|---|
| `files` | Per-file metadata: `file_path`, `file_mtime`, `title`, `description` |
| `sections` | Canonical section records with path, byte offsets, heading, content |
| `sections_fts` | FTS5 virtual table (trigger-maintained) |
| `sections_vec` | vec0 virtual table for 384-dim embeddings (populated by sidecar) |

`sections.file_path` has `ON DELETE CASCADE` to `files`, so deleting a file row automatically removes all its sections (and FTS5 triggers fire automatically). `sections_vec` rows must be deleted explicitly before the `files` row since virtual tables do not participate in foreign key cascades.

---

## Development

```sh
mise run build    # build binary
mise run test     # run all tests
mise run lint     # go vet
mise run clean    # remove build artifacts
mise run run      # build + start daemon (requires MEMORY_MD_DIR)
```

### Project layout

```
memory-md/
  main.go                    CLI entry point; subcommand dispatch
  internal/
    pathenc/pathenc.go       Encode MEMORY_MD_DIR → cache dir name
    parser/parser.go         goldmark walk + heading stack → ParseResult
    parser/parser_test.go
    db/db.go                 Open() + applySchema()
    db/sqlite_vec.go         init() registers vec0 on all connections
    engine/engine.go         Get, Search, New, Update, Delete, CreateFile, DeleteFile
    watcher/watcher.go       fsnotify loop + 500ms debounce
    rrf/rrf.go               Reciprocal Rank Fusion (K=60)
  daemon/daemon.go           Unix socket server + sidecar lifecycle
  sidecar/embed.py           Python embedding server (mlx-embeddings)
  sidecar/sidecar.go         //go:embed embed.py + Unix socket Client
  docs/MEMORY_MD_PLAN.md     Detailed design document
  mise.toml                  Tool versions and task definitions
  go.mod / go.sum
```

### Key design decisions

- **`zig cc` as the C compiler** — satisfies CGo without requiring Xcode or any system-installed C toolchain on macOS.
- **`-tags sqlite_fts5`** — required to include FTS5 in the bundled `go-sqlite3` amalgamation.
- **`sqlite_vec.Auto()`** in an `init()` — registers the `vec0` extension on every SQLite connection via `sqlite3_auto_extension`, requiring no per-connection setup.
- **DSN params for SQLite pragmas** — `?_journal_mode=WAL&_foreign_keys=true` are applied to every connection in the pool, not just the first.
- **`heading.Pos()`** for byte offsets — the goldmark block parser sets `node.Pos()` to the byte offset of the first `#` character of an ATX heading; no backward scan needed.
- **Content field stores immediate body only** — child section text is excluded, keeping FTS results precise.
- **Three byte-offset fields per section** — `HeadingStartByte` / `StartByte` / `EndByte` enable surgical splices for `update` (body only) and `delete` (full subtree).

---

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.
