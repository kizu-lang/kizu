#!/usr/bin/env python3
"""Tests scripts/comment-lint/check.py against the trees under fixtures/.

    python3 scripts/comment-lint/test.py

fixtures/ is laid out like the repository, so a file's path decides its
rule the same way it does for the real tree: compiler/src/ takes the strict
rule, lib/kizu/std/src/ also lets a contract comment pass. The docstring of
check.py defines the rule; every case it names has a line here.
"""

import os
import shutil
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.dont_write_bytecode = True
sys.path.insert(0, HERE)

import check  # noqa: E402

FIXTURES = os.path.join(HERE, "fixtures")


def lint(root, path):
    return check.check_file(root, path, check.TypeIndex(os.path.join(root, check.STD_PREFIX)))


class Passing(unittest.TestCase):
    def test_compiler_allows_doc_safety_and_strings(self):
        self.assertEqual(lint(FIXTURES, "compiler/src/pass/pass.kizu"), [])

    def test_std_allows_contracts_doc_safety_and_strings(self):
        self.assertEqual(lint(FIXTURES, "lib/kizu/std/src/good/good.kizu"), [])

    def test_std_without_comments(self):
        self.assertEqual(lint(FIXTURES, "lib/kizu/std/src/other/other.kizu"), [])


class Failing(unittest.TestCase):
    def test_compiler_refuses_every_plain_comment(self):
        self.assertEqual(lint(FIXTURES, "compiler/src/fail/fail.kizu"), [
            (1, "comment"),
            (3, "comment"),
            (4, "comment after code"),
            (7, "comment"),
        ])

    def test_std_refuses_comments_off_contracts(self):
        self.assertEqual(lint(FIXTURES, "lib/kizu/std/src/bad/bad.kizu"), [
            (1, "comment"),
            (3, "comment"),
            (9, "comment"),
            (14, "comment"),
            (19, "comment"),
            (24, "comment"),
            (29, "comment"),
            (35, "comment"),
            (36, "comment after code"),
            (40, "comment"),
        ])


class Mutations(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        shutil.copytree(FIXTURES, self.root, dirs_exist_ok=True)
        self.path = "lib/kizu/std/src/good/good.kizu"

    def tearDown(self):
        shutil.rmtree(self.root)

    def rewrite(self, old, new):
        full = os.path.join(self.root, self.path)
        with open(full, encoding="utf-8") as f:
            text = f.read()
        self.assertIn(old, text)
        with open(full, "w", encoding="utf-8") as f:
            f.write(text.replace(old, new, 1))

    def test_internal_comment_added_to_a_body_fails(self):
        self.rewrite("    return Thing { count: 0 };\n",
                     "    // nothing to allocate yet\n    return Thing { count: 0 };\n")
        self.assertEqual(lint(self.root, self.path), [(11, "comment")])

    def test_contract_on_a_declaration_made_private_fails(self):
        self.rewrite("pub fn new()", "fn new()")
        self.assertEqual(lint(self.root, self.path), [(7, "comment"), (8, "comment"), (9, "comment")])

    def test_method_of_a_type_made_private_fails(self):
        self.rewrite("pub struct Thing", "struct Thing")
        self.assertEqual(lint(self.root, self.path), [(1, "comment"), (14, "comment"), (19, "comment")])

    def test_blank_line_between_contract_and_declaration_fails(self):
        self.rewrite("// Error names what can go wrong.\n", "// Error names what can go wrong.\n\n")
        self.assertEqual(lint(self.root, self.path), [(34, "comment")])

    def test_same_file_under_compiler_src_fails(self):
        target = os.path.join(self.root, "compiler/src/good")
        os.makedirs(target)
        shutil.copy(os.path.join(self.root, self.path), target)
        lines = [line for line, _ in lint(self.root, "compiler/src/good/good.kizu")]
        self.assertEqual(lines, [1, 3, 7, 8, 9, 14, 19, 24, 29, 34, 39])


class Main(unittest.TestCase):
    def test_exit_status(self):
        self.assertEqual(check.main(["--root", FIXTURES, "compiler/src/pass/pass.kizu"]), 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
