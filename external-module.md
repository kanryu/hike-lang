# External Modules

Hike supports external Hike libraries through the `hikec get` subcommand.
Applications declare their dependencies in `hike.mod`, and `hikec get`
downloads those dependencies into a project-local `.hike/deps` directory.
This keeps external source trees separate from the application while allowing
the normal Hike package importer to resolve them by module path.

## Declaring a dependency

Use a `require` directive in `hike.mod`:

```text
module triangle-import

hike 0.1.0

require github.com/kanryu/hike-gpu-webgpu v0.1.0
```

The module path is also the dependency directory layout. The example above is
installed at:

```text
.hike/deps/github.com/kanryu/hike-gpu-webgpu
```

Imports then use the module path and a package path inside that module:

```hike
import "github.com/kanryu/hike-gpu-webgpu/gpu/webgpu"
```

## `hikec get` forms

### Install every declared dependency

Run `hikec get` without arguments from the project directory:

```bash
hikec get
```

Every `require` directive is processed in the order in which it appears in
`hike.mod`. This is the usual setup command for a project.

### Install a repository as the latest version

Specify a module path without a version:

```bash
hikec get github.com/kanryu/hike-gpu-webgpu
```

The repository is treated as the latest version and cloned from:

```text
https://github.com/kanryu/hike-gpu-webgpu.git
```

The resulting `require` entry uses `latest` when the module did not already
have a version in `hike.mod`.

### Install an exact release version

Specify a release with `@version`:

```bash
hikec get github.com/kanryu/hike-gpu-webgpu@v0.1.0
```

This form does not clone the Git repository. Instead, `hikec` downloads the
versioned source archive from the repository's release/tag archive, extracts
the source tree, and installs it under `.hike/deps`:

```text
https://github.com/kanryu/hike-gpu-webgpu/archive/refs/tags/v0.1.0.tar.gz
```

The matching `require` directive is automatically added or rewritten to the
requested version.

## Replacement behavior

When a module path or an explicit release version is supplied on the command
line, an existing directory for that module is removed before the new source
is installed. This makes repeated commands deterministic and allows a project
to switch between the latest repository checkout and a pinned release.

For example:

```bash
hikec get github.com/kanryu/hike-gpu-webgpu@latest
hikec get github.com/kanryu/hike-gpu-webgpu@v0.1.0
```

The second command replaces the latest checkout with the `v0.1.0` source
archive and updates `hike.mod` accordingly.

When `hikec get` is run without arguments, dependencies declared in `hike.mod`
are installed using their recorded versions. Existing versioned dependencies
are refreshed from their source archives; a dependency recorded as `latest` is
obtained as a repository checkout.

## Build workflow

External modules must be installed before compiling a project that imports
them:

```bash
hikec get
hikec build -target wasm32 main.hike -o main.wasm
```

If the required module is missing from `.hike/deps`, package resolution fails
and the project cannot be built.

The `.hike/` directory is intended to be project-local dependency state and
should normally be excluded from version control. `hike.mod` remains the
reproducible declaration of the external modules required by the project.

## Relative package replacements

When a package should be resolved from a directory relative to the project,
`replace` remains supported:

```text
replace github.com/kanryu/hike-gpu-webgpu => ../../gpu/webgpu
```

The path is resolved from the project module root. This resolves the import
directly from the relative directory and does not download or overwrite
`.hike/deps`. Use `require` and `hikec get` when the project should manage an
external module copy.

## Current scope

The source-archive path currently supports GitHub module paths. Repository
checkout mode uses Git. Release mode invokes the platform's `curl` command to
download the tagged archive and `tar` to extract it, then installs only source
files and does not leave a `.git` directory in the dependency tree. Both
commands must be available on the host when installing an exact release.
