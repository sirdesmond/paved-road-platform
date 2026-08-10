# Runbook 0002 — Docker build fails with "package cmd/main.go is not in std"

**Symptom:** `make docker-build` fails partway through:

```
Step 9/14 : RUN CGO_ENABLED=0 GOOS=linux GOARCH= go build -a -o manager cmd/main.go
package cmd/main.go is not in std (/usr/local/go/src/cmd/main.go)
The command '/bin/sh -c ... go build ...' returned a non-zero code: 1
```

**What it looks like:** a Go problem. The message names Go, the standard library, and a path inside the Go
installation. Every instinct says go and look at the module.

**What it actually is:** Docker sent an empty build context, so `cmd/main.go` doesn't exist inside the
container. Go falls back to treating the argument as an *import path* rather than a file, can't find a
package by that name, and reports that it isn't in the standard library. The error is two layers away from
the cause.

## Diagnosis

Add a line above the build step in the `Dockerfile`:

```dockerfile
RUN ls -R cmd api internal
```

and rebuild. If you get `ls: cannot access 'cmd': No such file or directory`, the source never made it into
the image and nothing about your Go setup is wrong.

(`docker build --progress=plain` shows the same thing, but only on BuildKit. On the classic builder the flag
doesn't exist, which is itself a clue — see below.)

## Cause

Recent kubebuilder scaffolds ship a `.dockerignore` in the "exclude everything, then re-include" style:

```
**
!**/*.go
!go.mod
!go.sum
```

**BuildKit** evaluates the exceptions correctly and the build works.

**The classic builder** excludes the *directories* via `**` and does not descend into an excluded directory,
so the files inside are never evaluated against `!**/*.go`. The re-include never gets a chance to apply, and
the context arrives empty.

Which builder you get depends on the Docker version and whether BuildKit is enabled — so the same repo builds
on a colleague's machine and not yours. On Colima and older Docker CLIs you'll typically be on the classic
builder. Quick check: if `docker build --progress=plain` errors with `unknown flag`, you're on the classic
builder.

## Fix

Replace the `.dockerignore` with an explicit exclude list, which behaves the same on both builders:

```
bin/
dist/
cover.out
.git/
.vscode/
.idea/
.DS_Store
test/e2e/
*.md
```

While you're there, make the build target unambiguous:

```dockerfile
# was: go build -a -o manager cmd/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd
```

A bare `cmd/main.go` is only treated as a file if it resolves as one; otherwise Go reads it as an import
path, which is what produced the misleading message. `./cmd` can only mean a package directory, and it also
survives the day someone splits `main.go` into two files — a file-list build would silently miss the second.

## Prevention

- Prefer explicit exclude lists in `.dockerignore`. The re-include style is clever, and its failure mode is an empty context with a confusing error rather than a clear one.
- Use `./cmd` (or `./cmd/...`) in Dockerfiles rather than a bare file path.
- If a build works in CI but not locally (or vice versa), check which builder each is using before suspecting the code.

## The general lesson

Worth keeping: **the layer that reports an error is often not the layer that caused it.** Go reported a
missing package, Docker caused it by shipping an empty context, and the trigger was which build engine was in
use. When an error names a subsystem you haven't touched, check what that subsystem was *given* before
debugging the subsystem itself.
