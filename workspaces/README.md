# Workspaces

This directory contains project data accessed by MCP tools.

Each subdirectory should represent one logical workspace:

```
workspaces/
  project-a/
  book-b/
```

The gateway exposes this directory to tools as:

```
/workspaces
```

Notes:

- Tools should never access files outside this directory.
- Use subfolders to isolate projects.
- This directory is mounted read-write.
