---
name: memory-md
description: Store, retrieve, and search persistent memory backed by plain markdown files. Use when you need to remember decisions, facts, or context across sessions — or when the user asks you to recall, save, or look up something from memory.
compatibility: Requires the memory-md binary in PATH, MEMORY_MD_DIR env var set, and the daemon running (memory-md start-daemon). Optionally requires uv for vector search on Apple Silicon.
allowed-tools: Bash
---

# memory-md skill

Use this skill when you need to store, retrieve, or search persistent memory backed by markdown files.

## Prerequisites

- `MEMORY_MD_DIR` must be set to the memory directory path.
- The daemon must be running (`memory-md start-daemon`) for all commands except `status`, `snapshot`, `validate-file`, and `version`.
- Check first with `status` before assuming the daemon is up.

## Check daemon status

```sh
memory-md status
```

Output when running:
```
daemon:  running  (/your/notes)
sidecar: active   (vector search enabled)
```

Output when not running:
```
daemon:  not running
```

If not running, start it in the background before proceeding:
```sh
memory-md start-daemon &
```

---

## Find the cache directory

The cache directory (SQLite index, socket) uses a SHA-256 hash of `MEMORY_MD_DIR` as its name. To identify which hashed directory belongs to which memory dir:

```sh
# Show all cache dirs and their corresponding MEMORY_MD_DIR paths
cat ~/.cache/memory-md/*/dir

# Find the cache dir for a specific path
grep -rl "/your/notes" ~/.cache/memory-md/*/dir
```

Each cache directory contains a `dir` file with the original `MEMORY_MD_DIR` path in plain text.

---

## Browse what's stored

```sh
# List all files
memory-md list

# List all sections within a file (in document order)
memory-md list auth
```

Output of `list auth`:
```
auth/api-keys
auth/api-keys/rotation-policy
```

---

## Store a memory

Always create the file first if it doesn't exist, then add sections. `create-file` requires a file name plus a title for the file's `#` heading, and can also take an optional description placed below that title.

```sh
# Create a file (once per topic area)
memory-md create-file auth "Authentication" "Covers auth-related decisions."

# Add a top-level section
echo "Keys are hashed with bcrypt before storage." \
  | memory-md new auth/api-keys --heading "API Keys"

# Add a nested section
echo "Keys rotate every 90 days." \
  | memory-md new auth/api-keys/rotation-policy --heading "Rotation Policy"
```

- Path segments are slugified heading text: `"API Keys"` → `api-keys`
- `--heading` sets the human-readable heading in the file; omit it to use the slug as-is
- Nesting is unlimited: `auth/api-keys/rotation-policy/details` → `####`
- `new` fails if the section already exists — use `update` to overwrite
- After `new` writes the section, `memory-md` immediately validates the target `.md` file and prints any validation errors without rolling the write back

---

## Retrieve a memory

```sh
# Exact path lookup
memory-md get auth/api-keys
```

Output:
```
API Keys

Keys are hashed with bcrypt before storage.
```

---

## Search memories

```sh
memory-md search "key rotation policy" --top 5
```

- Hybrid FTS5 + vector search when the sidecar is active; FTS5-only otherwise
- `--top` defaults to 5

Output:
```
=== auth/api-keys ===
API Keys

Keys are hashed with bcrypt before storage.

=== auth/api-keys/rotation-policy ===
Rotation Policy

Keys rotate every 90 days.
```

---

## Update a memory

Replaces the immediate body of a section. Child sections are preserved.

After `update` writes the new body, `memory-md` immediately validates the target `.md` file and prints any validation errors without rolling the edit back.

```sh
echo "Updated content here." | memory-md update auth/api-keys
```

---

## Delete a memory

Deletes a section and all its children.

```sh
memory-md delete auth/api-keys          # removes api-keys and rotation-policy
memory-md delete-file auth              # removes the entire auth.md file
```

---

## Path conventions

- File name = first path segment. `auth.md` → paths start with `auth/`
- Heading level = number of path segments: 2 → `##`, 3 → `###`, etc.
- Slugification: lowercase, spaces → `-`, non-alphanumeric stripped
  - `"API Keys"` → `api-keys`
  - `"Token Refresh Policy"` → `token-refresh-policy`
- The `#` title heading in a file is decorative metadata — not part of any path

---

## Decision guide

| Situation | Command |
|---|---|
| Don't know what files exist | `list` |
| Don't know what sections exist in a file | `list <name>` |
| Don't know if something exists | `search` first, then `get` if you find the exact path |
| Know the exact path | `get` |
| Storing new information | `new` (create-file first if needed) |
| Correcting or updating existing content | `update` |
| Removing outdated information | `delete` or `delete-file` |
| Backing up before bulk changes | `snapshot` |
| Archiving root-level markdown files into a snapshot dir | `snapshot --move` |

---

## Error reference

| Error | Meaning |
|---|---|
| `cannot connect to daemon` | Daemon not running — run `memory-md start-daemon` |
| `section already exists` | Use `update` instead of `new` |
| `section not found` | Path doesn't exist — use `search` to find the right path |
| `file not found` | Run `create-file <name> <title> [description]` first |
| `parent section not found` | Create the parent section before the child |
| `file already exists` | `create-file` target already exists — use `new` to add sections to it |
