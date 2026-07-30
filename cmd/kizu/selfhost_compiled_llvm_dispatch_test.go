package main

import (
	"strings"
	"testing"
)

// TestSelfhostCompiledLLVMGuardsFormatOnlyStructuredCFShapes keeps formatter-only
// structured-control-flow detection out of every other compiled component.
func TestSelfhostCompiledLLVMGuardsFormatOnlyStructuredCFShapes(t *testing.T) {
	compiled := readSelfhostFile(t, "../../selfhost/src/backend/compiled_llvm.kizu")
	// append_compiled_function_params_indexed is now a wrapper; the dispatch body
	// it used to hold lives in the context-taking variant.
	body := selfhostKizuFunctionBody(
		t, compiled, "fn append_compiled_function_params_context_indexed(",
	)
	guard := "if compiled_function_is_format_component(function_name) {"
	guardIndex := strings.Index(body, guard)
	if guardIndex < 0 {
		t.Fatalf("compiled LLVM dispatch missing format component guard %q", guard)
	}
	for _, shape := range []string{
		"compiled_struct_cf::is_import_sort_shape(",
		"compiled_struct_cf::is_leading_import_shape(",
		"compiled_struct_cf::is_import_decl_shape(",
		"compiled_struct_cf::is_append_indent_shape(",
		"compiled_struct_cf::is_sorted_imports_shape(",
		"compiled_struct_cf::append_comment_preserve_function(",
	} {
		shapeIndex := strings.Index(body, shape)
		if shapeIndex < 0 {
			t.Fatalf("compiled LLVM dispatch missing structured-CF shape %q", shape)
		}
		if shapeIndex < guardIndex {
			t.Fatalf("structured-CF shape %q is not guarded by the format component prefix", shape)
		}
	}

	helper := selfhostKizuFunctionBody(t, compiled, "fn compiled_function_is_format_component(")
	formatPrefix := `std::mem::starts_with(function_name, "selfhost::parser::format::")`
	if !strings.Contains(helper, formatPrefix) {
		t.Fatal("format component guard must be prefix-limited to selfhost::parser::format")
	}
}
