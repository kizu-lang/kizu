#!/usr/bin/env python3
"""Checks the comment rule of AGENTS.md on Kizu sources.

    python3 scripts/comment-lint/check.py [--root DIR] FILE...

FILE is relative to DIR (default: the current directory), the way
pre-commit passes it. Exit status 1 and one `path:line: reason` per
violation when a file breaks the rule.

What counts as a comment follows compiler/src/internal/lexer: `"` opens a
string that runs to the next `"` (there are no escapes), `\\` opens a
multiline-string line that runs to the end of the line, and `//` outside
both opens a comment that runs to the end of the line.

Every file:
  - `///` (exactly three slashes) passes: the language asks for it on
    unsafe declarations.
  - `// SAFETY:` passes, at the start of a line or after code.
  - every other `//` fails, at the start of a line or after code.

A file under lib/kizu/std/src/, outside lib/kizu/std/src/internal/,
additionally lets a contract comment pass:
a block of consecutive line-start `//` lines (`//` alone and `///` lines
included) whose next line, with no blank line between, is a contract
declaration:
  - a line that starts with `pub` (a top-level declaration or a field);
  - a receiver method, `fn (self: T) name` (optionally `unsafe fn`), whose
    receiver type T, after `&` / `&var` and generic arguments, is public:
      * a `std::m::Name` path whose module directory lib/kizu/std/src/m/
        declares `pub struct|enum|union|error Name`, or does not declare
        Name at all (a type the compiler knows, such as std::arena::Arena);
      * a bare `Name` that the file's own module directory declares `pub`.
    A receiver type the module declares without `pub`, or a bare name it
    does not declare, makes the method internal.
Any other comment in std fails like in compiler/src: one on an internal
declaration, one inside a body, one after code, and one followed by a
blank line or the end of the file. std::internal is not an outward API,
so a file under lib/kizu/std/src/internal/ takes the compiler/src rule
whole: a `//` block above a `pub` declaration fails there too.
"""

import argparse
import os
import re
import sys

STD_PREFIX = "lib/kizu/std/src/"
STD_INTERNAL_PREFIX = STD_PREFIX + "internal/"

TYPE_DECL = re.compile(r"^(pub\s+)?(?:unsafe\s+)?(?:struct|enum|union|error)\s+([A-Za-z_][A-Za-z0-9_]*)")
RECEIVER = re.compile(r"^(?:unsafe\s+)?fn\s*\(\s*self\s*:\s*(?:&\s*(?:var\s+)?)?([A-Za-z_][A-Za-z0-9_:]*)")
SAFETY = re.compile(r"^//[ \t]*SAFETY:")


class Comment:
    def __init__(self, line, text, leading):
        self.line = line
        self.text = text
        self.leading = leading


def scan_comments(source):
    comments = []
    i = 0
    n = len(source)
    line = 1
    line_has_code = False
    while i < n:
        ch = source[i]
        if ch == "\n":
            line += 1
            line_has_code = False
            i += 1
        elif ch == '"':
            line_has_code = True
            i += 1
            while i < n and source[i] != '"':
                if source[i] == "\n":
                    line += 1
                i += 1
            i += 1
        elif source.startswith("\\\\", i):
            line_has_code = True
            end = source.find("\n", i)
            i = n if end < 0 else end
        elif source.startswith("//", i):
            end = source.find("\n", i)
            end = n if end < 0 else end
            comments.append(Comment(line, source[i:end], not line_has_code))
            i = end
        else:
            if ch not in " \t\r":
                line_has_code = True
            i += 1
    return comments


def is_allowed_everywhere(text):
    if text.startswith("///") and not text.startswith("////"):
        return True
    return SAFETY.match(text) is not None


class TypeIndex:
    def __init__(self, std_root):
        self.std_root = std_root
        self.cache = {}

    def declarations(self, module):
        if module not in self.cache:
            found = {}
            directory = os.path.join(self.std_root, module)
            if os.path.isdir(directory):
                for name in sorted(os.listdir(directory)):
                    if not name.endswith(".kizu"):
                        continue
                    with open(os.path.join(directory, name), encoding="latin-1") as f:
                        for text in f:
                            match = TYPE_DECL.match(text)
                            if match:
                                public = match.group(1) is not None
                                found[match.group(2)] = found.get(match.group(2), False) or public
            self.cache[module] = found
        return self.cache[module]

    def is_public(self, type_path, own_module):
        segments = type_path.split("::")
        if len(segments) == 1:
            return self.declarations(own_module).get(segments[0], False)
        if segments[0] != "std" or len(segments) < 3:
            return False
        declared = self.declarations("/".join(segments[1:-1]))
        return declared.get(segments[-1], True)


def is_contract_declaration(text, types, own_module):
    stripped = text.strip()
    if re.match(r"^pub\b", stripped):
        return True
    match = RECEIVER.match(stripped)
    return match is not None and types.is_public(match.group(1), own_module)


def contract_lines(lines, comments, types, own_module):
    leading = {c.line for c in comments if c.leading}
    allowed = set()
    line = 1
    while line <= len(lines):
        if line not in leading:
            line += 1
            continue
        start = line
        while line in leading:
            line += 1
        if line <= len(lines) and is_contract_declaration(lines[line - 1], types, own_module):
            allowed.update(range(start, line))
    return allowed


def check_file(root, path, types):
    with open(os.path.join(root, path), encoding="latin-1") as f:
        source = f.read()
    comments = scan_comments(source)
    normalized = path.replace(os.sep, "/")
    allowed = set()
    if normalized.startswith(STD_PREFIX) and not normalized.startswith(STD_INTERNAL_PREFIX):
        own_module = os.path.dirname(normalized[len(STD_PREFIX):])
        allowed = contract_lines(source.split("\n"), comments, types, own_module)
    violations = []
    for comment in comments:
        if is_allowed_everywhere(comment.text):
            continue
        if not comment.leading:
            violations.append((comment.line, "comment after code"))
        elif comment.line not in allowed:
            violations.append((comment.line, "comment"))
    return violations


def main(argv):
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".")
    parser.add_argument("files", nargs="*")
    args = parser.parse_args(argv)
    types = TypeIndex(os.path.join(args.root, STD_PREFIX))
    failed = False
    for path in args.files:
        for line, reason in check_file(args.root, path, types):
            print(f"{path}:{line}: {reason}")
            failed = True
    if failed:
        sys.stdout.flush()
        print("compiler/src and lib/kizu/std carry no explanatory comments (AGENTS.md):"
              " `///`, `// SAFETY:` and, in std, a `//` block right above a pub"
              " declaration or a public type's method are the only comments.",
              file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
