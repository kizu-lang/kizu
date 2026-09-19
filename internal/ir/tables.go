package ir

import (
	"strconv"
	"strings"
)

// FoldConstantTables rewrites each function that compares its one integer
// parameter against 0, 1, 2, ... in turn and returns a constant for each
// into one table.get. Kizu has no constant arrays, so a table (the kernels
// of std::math) is such a function, and left as a chain of comparisons it
// costs the wasm target a comparison per entry on every call. table.get
// carries the values in order, the fall-through's last, and answers
// values[index] for index below len - 1 and the last value otherwise.
func FoldConstantTables(module *Module) {
	for _, fn := range module.Functions {
		values, ok := constantTable(fn)
		if !ok {
			continue
		}
		last := fn.Blocks[len(fn.Blocks)-1].Terminator.Value
		result := Value{Name: last.Name, Type: fn.Return}
		get := &Instr{
			Result:    result,
			Op:        "table.get",
			Args:      []Value{{Name: fn.Params[0].Name, Type: fn.Params[0].Type}},
			Immediate: formatTable(values, fn.Return),
		}
		fn.Blocks = []*Block{{
			Name:       fn.Blocks[0].Name,
			Instrs:     []*Instr{get},
			Terminator: Terminator{Op: "return", Value: result},
		}}
	}
}

// constantTable reads the values of a function shaped as a table: a chain
// of blocks each comparing the parameter with the next integer and
// returning a constant on a match, ending in a block returning a constant.
// At least two values make a table.
func constantTable(fn *Function) ([]uint64, bool) {
	if len(fn.Params) != 1 || fn.Params[0].Type != "i64" || !isTableType(fn.Return) {
		return nil, false
	}
	blocks := map[string]*Block{}
	for _, block := range fn.Blocks {
		blocks[block.Name] = block
	}
	values := []uint64{}
	block := fn.Blocks[0]
	for block.Terminator.Op != "return" {
		value, next, ok := tableStep(fn, blocks, block, len(values))
		if !ok {
			return nil, false
		}
		values = append(values, value)
		block = next
	}
	value, ok := evaluateBlock(block, fn.Return)
	if !ok || len(values) < 1 {
		return nil, false
	}
	return append(values, value), true
}

// tableStep reads one link of a table's chain: the block must compare the
// parameter with key, return a constant when they match and otherwise jump
// on. It returns that constant and the block jumped to.
func tableStep(
	fn *Function, blocks map[string]*Block, block *Block, key int,
) (uint64, *Block, bool) {
	if !comparesParamWith(fn, block, key) {
		return 0, nil, false
	}
	then, other := blocks[block.Terminator.Target], blocks[block.Terminator.Else]
	if then == nil || other == nil || then.Terminator.Op != "return" {
		return 0, nil, false
	}
	value, ok := evaluateBlock(then, fn.Return)
	if !ok || len(other.Instrs) != 0 || other.Terminator.Op != "jump" {
		return 0, nil, false
	}
	next := blocks[other.Terminator.Target]
	return value, next, next != nil
}

// comparesParamWith reports whether a block does nothing but branch on
// whether the function's parameter equals key.
func comparesParamWith(fn *Function, block *Block, key int) bool {
	if block.Terminator.Op != "branch" || len(block.Instrs) != 2 {
		return false
	}
	constant, compare := block.Instrs[0], block.Instrs[1]
	if constant.Op != "const" || constant.Result.Type != "i64" ||
		constant.Immediate != strconv.Itoa(key) {
		return false
	}
	return compare.Op == "binary.==" && len(compare.Args) == 2 &&
		compare.Args[0].Name == fn.Params[0].Name &&
		compare.Args[1].Name == constant.Result.Name &&
		block.Terminator.Cond.Name == compare.Result.Name
}

// isTableType reports whether a table can hold values of the type.
func isTableType(typ string) bool {
	return typ == "u64" || typ == "i64"
}

// evaluateBlock computes the constant a block returns: its instructions
// must be integer constants and the wrapping arithmetic of the table type
// on them, and the returned value one of those.
func evaluateBlock(block *Block, typ string) (uint64, bool) {
	known := map[string]uint64{}
	for _, instr := range block.Instrs {
		if instr.Result.Type != typ {
			return 0, false
		}
		var value uint64
		switch instr.Op {
		case "const":
			parsed, ok := parseTableConstant(instr.Immediate, typ)
			if !ok {
				return 0, false
			}
			value = parsed
		default:
			if len(instr.Args) != 2 {
				return 0, false
			}
			left, okLeft := known[instr.Args[0].Name]
			right, okRight := known[instr.Args[1].Name]
			if !okLeft || !okRight {
				return 0, false
			}
			computed, ok := tableArithmetic(instr.Op, left, right, typ)
			if !ok {
				return 0, false
			}
			value = computed
		}
		known[instr.Result.Name] = value
	}
	value, ok := known[block.Terminator.Value.Name]
	return value, ok
}

// parseTableConstant reads an integer literal as the bits of the table type.
func parseTableConstant(text string, typ string) (uint64, bool) {
	if typ == "u64" {
		value, err := strconv.ParseUint(text, 10, 64)
		return value, err == nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return uint64(value), err == nil
}

// tableArithmetic computes the wrapping integer operations a table value is
// built from: shifts, which give 0 past the width, and bit and sum
// operations.
func tableArithmetic(op string, left uint64, right uint64, typ string) (uint64, bool) {
	switch op {
	case "binary.<<":
		if right >= 64 {
			return 0, true
		}
		return left << right, true
	case "binary.>>":
		if typ != "u64" {
			return 0, false
		}
		if right >= 64 {
			return 0, true
		}
		return left >> right, true
	case "binary.|":
		return left | right, true
	case "binary.&":
		return left & right, true
	case "binary.^":
		return left ^ right, true
	case "binary.+":
		return left + right, true
	case "binary.-":
		return left - right, true
	case "binary.*":
		return left * right, true
	}
	return 0, false
}

// formatTable spells table values as the immediate of table.get: decimal,
// signed for i64, separated by commas.
func formatTable(values []uint64, typ string) string {
	texts := make([]string, len(values))
	for i, value := range values {
		if typ == "u64" {
			texts[i] = strconv.FormatUint(value, 10)
		} else {
			texts[i] = strconv.FormatInt(int64(value), 10)
		}
	}
	return strings.Join(texts, ",")
}

// TableValues reads the values a table.get carries, as the bits of each.
func TableValues(instr *Instr) []uint64 {
	texts := strings.Split(instr.Immediate, ",")
	values := make([]uint64, len(texts))
	for i, text := range texts {
		if instr.Result.Type == "u64" {
			values[i], _ = strconv.ParseUint(text, 10, 64)
		} else {
			signed, _ := strconv.ParseInt(text, 10, 64)
			values[i] = uint64(signed)
		}
	}
	return values
}
