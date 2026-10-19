---
title: Experimental imports and gomplate
nav_order: 4
---

# Experimental Butane imports and gomplate

This page describes the `v0.1.0-experimental.1` proof of concept from the
[`vic1707/ignition` experimental release](https://github.com/vic1707/ignition/releases/tag/v0.1.0-experimental.1).
It adds recursive Butane imports and an optional external file processor to Butane.
The generated output is ordinary Ignition JSON; the features run while transpiling,
not on the target machine.

This is an **experimental, vibe-coded proof of concept adapted from existing work
and pull requests**. It is intended for trying the features on real CoreOS systems,
but it has not been validated through live provisioning. Interfaces and behavior may
change without notice.

## Install

Install the prerelease with [mise's GitHub backend](https://mise.jdx.dev/dev-tools/backends/github.html):

```sh
mise install 'github:vic1707/ignition[matching=butane,prerelease=true]@0.1.0-experimental.1'
```

The archive provides the `butane` executable. Gomplate is a separate program. This
guide uses Gomplate 5.2.0:

```sh
mise install gomplate@5.2.0
```

## Supported specs

| Variant | Butane spec | Generated Ignition spec |
|---|---|---|
| `fcos` stable | `1.7.0` | `3.6.0` |
| `fcos` experimental | `1.8.0-experimental` | `3.7.0-experimental` |
| `flatcar` stable | `1.1.0` | `3.4.0` |
| `flatcar` experimental | `1.2.0-experimental` | `3.7.0-experimental` |

Imported documents must use exactly the same `variant` and `version` as the
containing document. This release does not convert between versions. Use an
Ignition version supported by the operating system image you plan to boot.

## Import another Butane config

A resource can use either `inline_butane` or `local_butane` in place of its other
content sources. An inline import contains a complete Butane document as a string:

```yaml
variant: fcos
version: 1.7.0
ignition:
  config:
    merge:
      - inline_butane: |
          variant: fcos
          version: 1.7.0
          storage:
            files:
              - path: /etc/example-fragment
                mode: 0644
                contents:
                  inline: imported from an inline Butane document
```

A local import points to another `.bu` file:

```yaml
variant: fcos
version: 1.7.0
ignition:
  config:
    merge:
      - local_butane:
          path: imports/example.bu
```

`local_butane.path` is resolved relative to the directory containing the importing
`.bu` file, so `../` and sibling directories work like ordinary source imports.
This is independent of `--files-dir`. If Butane reads its root document from stdin,
the current working directory is used as its source directory.

The optional `files_dir` selects the directory for ordinary local file references
inside that imported document. When omitted, it defaults to the imported `.bu` file's
directory. When provided, it is resolved relative to that same directory. Nested
imports use their own source file directories. Inline imported documents inherit the
containing document's source and resource directories. Ordinary local resources in
the root document continue to use `--files-dir`:

```sh
butane --files-dir . --pretty root.bu > root.ign
```

For example, given `root.bu` importing `imports/site.bu`, the default `files_dir`
for `site.bu` is `imports/`; a local resource `assets/banner` in that file reads
`imports/assets/banner`. To choose a different resource directory:

```yaml
ignition:
  config:
    merge:
      - local_butane:
          path: imports/site.bu
          files_dir: site-files
```

The selected resource directory is `imports/site-files`, relative to the directory
containing `site.bu`. Within the child document, local resources and tree sources
are read from that selected directory.

Imports are translated recursively and embedded in the resulting Ignition config.
They do not require an import feature on the target system. Imported configs must
match the parent's variant and version exactly.

## Render with gomplate

The generic processor hook runs an external program with each selected file on stdin
and uses its stdout as the file contents. Gomplate can read an optional `.gomplate.yaml`
configuration from the current working directory.

Use a Butane template such as `root.bu`:

```yaml
variant: fcos
version: 1.7.0
storage:
  files:
    - path: /etc/hostname
      mode: 0644
      contents:
        inline: '{{ .Env.COREOS_HOSTNAME }}'
```

Select the `bu` suffix and explicitly enable stdin processing if the root input is
stdin. The `stdin` entry only affects stdin; `*` matches named files:

```sh
COREOS_HOSTNAME=node-01 \
  mise x gomplate@5.2.0 \
    'github:vic1707/ignition[matching=butane,prerelease=true]@0.1.0-experimental.1' -- \
  butane --file-processor gomplate \
    --file-processor-allowed-ext bu,tmpl,stdin \
    --files-dir . --pretty root.bu > root.ign
```

Gomplate reads its config and templates relative to the process working directory.
Butane invokes the executable directly without a shell, so arguments cannot be added
to the processor command line; use a wrapper executable if you need fixed arguments.
An empty allowed-extension list leaves files unprocessed. Files with extensions not
listed are passed through unchanged. Processor errors fail the translation, and
processor stderr is shown to the caller.

The hook also processes selected imported `.bu` files and selected local resources
when Butane reads them. It does not reprocess the generated Ignition JSON from an
import. Gomplate must therefore emit valid Butane YAML when processing a `.bu` input.

## Proof-of-concept limits

- Recursive imports have no cycle detection or explicit depth guard. Cyclic or very deep
  imports can exhaust the stack, panic, or crash Butane.
- Only the four Butane/Ignition spec pairs listed above are supported by this
  release. Other variants and versions may fail or panic.
- Imports require exact matching parent and child variants and versions.
- This is a proof of concept; its schema and command-line interface may change.
- Gomplate is installed separately. The allowed suffixes must be selected explicitly,
  and Gomplate's working-directory configuration applies to every processed file.
- Experimental Ignition output requires a provisioning engine that supports that
  experimental Ignition version. The release has not been validated by booting
  machines or testing hardware-specific provisioning.

## Existing work and credit

This proof of concept assembles and adapts existing work, rather than claiming these
features as new designs:

- Butane imports: [coreos/butane #631](https://github.com/coreos/butane/pull/631),
  [coreos/butane #656](https://github.com/coreos/butane/pull/656), and
  [coreos/ignition #2300](https://github.com/coreos/ignition/pull/2300).
- Gomplate: [coreos/ignition #2298](https://github.com/coreos/ignition/pull/2298)
  and [coreos/ignition #2327](https://github.com/coreos/ignition/pull/2327).

Thanks to the authors, reviewers, and commenters on these PRs for the prior
implementation work and design discussion this release builds on.
