# ADR-0048: module import and manifest policy

## Status

Accepted.

## Context

Kizu needs a package marker before the compiler can resolve modules, cache a
package, or distinguish a package invocation from a loose source file. That
marker is bootstrap infrastructure: the shipping Go compiler and the eventual
self-hosted compiler must both be able to read it quickly and predictably.

## Rationale

Kizu uses a declarative `kizu.toml` manifest. TOML has a small, familiar subset
for package identity and source roots, and keeping executable logic out of the
manifest prevents package loading from becoming a second programming language.
The compiler can parse the required subset directly without a plugin runtime or
build-script dependency.

The manifest identifies the package and where source trees begin; it does not
list individual modules or visibility exceptions. Directory-derived module
identity and name resolution are justified in ADR-0049 and defined in
`SPEC.md`. This split keeps the manifest stable when implementation files are
added, removed, or divided.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Executable build scripts or compiler plugins in the manifest | Package discovery would run hidden control flow before ordinary compilation and would complicate bootstrap, review, and caching. |
| A custom manifest language | It adds a parser and tooling surface without expressing a requirement TOML cannot cover. |
| KDL | Its tree syntax is not needed for the current flat package metadata and costs another bootstrap parser. |
| List every module or source file in the manifest | Ordinary file splits would require synchronized configuration edits and make the filesystem cease to be the reviewable source of truth. |
