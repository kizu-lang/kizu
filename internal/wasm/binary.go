package wasm

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	wasmI32     = byte(0x7f)
	wasmI64     = byte(0x7e)
	wasmF32     = byte(0x7d)
	wasmF64     = byte(0x7c)
	wasmFuncRef = byte(0x70)
)

// Binary renders the executable binary form of this module.
func (m *Module) Binary() ([]byte, error) {
	lowered, err := lowerBinaryModule(m)
	if err != nil {
		return nil, err
	}
	return lowered.encode()
}

type binaryFuncType struct {
	params  []byte
	results []byte
}

type binaryImport struct {
	module    string
	name      string
	typeIndex uint32
}

type binaryNamedType struct {
	name     string
	typeCode byte
}

type binaryFunction struct {
	node      int
	name      string
	typeIndex uint32
	params    []binaryNamedType
	locals    []binaryNamedType
}

type binaryTable struct {
	minimum uint32
}

type binaryMemory struct {
	minimum uint32
}

type binaryGlobal struct {
	name     string
	typeCode byte
	mutable  bool
	init     int
}

type binaryExport struct {
	name  string
	kind  byte
	index uint32
}

type binaryElement struct {
	offset    int
	functions []uint32
}

type binaryData struct {
	offset int
	bytes  []byte
}

type binaryModule struct {
	source        *Module
	types         []binaryFuncType
	typeNames     map[string]uint32
	imports       []binaryImport
	functions     []binaryFunction
	functionNames map[string]uint32
	tables        []binaryTable
	memories      []binaryMemory
	globals       []binaryGlobal
	globalNames   map[string]uint32
	exports       []binaryExport
	elements      []binaryElement
	data          []binaryData
}

// lowerBinaryModule resolves the module-wide index spaces before any function
// body is encoded, so forward calls and branches never depend on traversal
// order.
func lowerBinaryModule(source *Module) (*binaryModule, error) {
	module := &binaryModule{
		source:        source,
		typeNames:     map[string]uint32{},
		functionNames: map[string]uint32{},
		globalNames:   map[string]uint32{},
	}
	declarations := source.nodes[source.root].children[1:]
	if err := module.addTypeDeclarations(declarations); err != nil {
		return nil, err
	}
	if err := module.addImports(declarations); err != nil {
		return nil, err
	}
	if err := module.addFunctions(declarations); err != nil {
		return nil, err
	}
	if err := module.addRemainingDeclarations(declarations); err != nil {
		return nil, err
	}
	return module, nil
}

// addTypeDeclarations completes the type index space before imports refer to
// it.
func (m *binaryModule) addTypeDeclarations(declarations []int) error {
	for _, declaration := range declarations {
		if m.source.listHead(declaration) != "type" {
			continue
		}
		if err := m.addTypeDeclaration(declaration); err != nil {
			return err
		}
	}
	return nil
}

// addImports assigns the imported prefix of the function index space.
func (m *binaryModule) addImports(declarations []int) error {
	for _, declaration := range declarations {
		if m.source.listHead(declaration) != "import" {
			continue
		}
		if err := m.addImport(declaration); err != nil {
			return err
		}
	}
	return nil
}

// addFunctions assigns every defined function index before bodies, exports,
// and element segments resolve forward references.
func (m *binaryModule) addFunctions(declarations []int) error {
	for _, declaration := range declarations {
		if m.source.listHead(declaration) != "func" {
			continue
		}
		if err := m.addFunction(declaration); err != nil {
			return err
		}
	}
	return nil
}

// addRemainingDeclarations records sections that can refer to the completed
// type and function index spaces.
func (m *binaryModule) addRemainingDeclarations(declarations []int) error {
	for _, declaration := range declarations {
		var err error
		switch m.source.listHead(declaration) {
		case "type", "import":
			continue
		case "func":
			err = m.addFunctionExports(declaration)
		case "table":
			err = m.addTable(declaration)
		case "memory":
			err = m.addMemory(declaration)
		case "global":
			err = m.addGlobal(declaration)
		case "export":
			err = m.addExport(declaration)
		case "elem":
			err = m.addElement(declaration)
		case "data":
			err = m.addData(declaration)
		default:
			err = m.errorf(declaration, "unsupported module declaration")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// addTypeDeclaration registers one explicitly named call_indirect signature.
func (m *binaryModule) addTypeDeclaration(index int) error {
	children := m.source.nodes[index].children
	if len(children) != 3 || m.source.nodeKind(children[1]) != moduleAtom ||
		m.source.nodeKind(children[2]) != moduleList ||
		m.source.listHead(children[2]) != "func" {
		return m.errorf(index, "invalid type declaration")
	}
	name := m.source.nodeText(children[1])
	if !strings.HasPrefix(name, "$") {
		return m.errorf(children[1], "type name must start with `$`")
	}
	if _, exists := m.typeNames[name]; exists {
		return m.errorf(children[1], "duplicate type %s", name)
	}
	typeValue, _, _, err := m.parseFunctionType(children[2])
	if err != nil {
		return err
	}
	m.typeNames[name] = uint32(len(m.types))
	m.types = append(m.types, typeValue)
	return nil
}

// addImport registers one imported function and its function index.
func (m *binaryModule) addImport(index int) error {
	children := m.source.nodes[index].children
	if len(children) != 4 || m.source.nodeKind(children[1]) != moduleString ||
		m.source.nodeKind(children[2]) != moduleString ||
		m.source.listHead(children[3]) != "func" {
		return m.errorf(index, "invalid function import")
	}
	moduleName, err := m.source.stringBytes(children[1])
	if err != nil {
		return err
	}
	fieldName, err := m.source.stringBytes(children[2])
	if err != nil {
		return err
	}
	typeValue, params, _, err := m.parseFunctionType(children[3])
	if err != nil {
		return err
	}
	funcChildren := m.source.nodes[children[3]].children
	if len(funcChildren) < 2 || m.source.nodeKind(funcChildren[1]) != moduleAtom {
		return m.errorf(children[3], "imported function needs a name")
	}
	name := m.source.nodeText(funcChildren[1])
	if !strings.HasPrefix(name, "$") {
		return m.errorf(funcChildren[1], "function name must start with `$`")
	}
	if len(params) != len(typeValue.params) {
		return m.errorf(children[3], "invalid imported function parameters")
	}
	if _, exists := m.functionNames[name]; exists {
		return m.errorf(funcChildren[1], "duplicate function %s", name)
	}
	typeIndex := m.internType(typeValue)
	functionIndex := uint32(len(m.imports))
	m.functionNames[name] = functionIndex
	m.imports = append(m.imports, binaryImport{
		module:    string(moduleName),
		name:      string(fieldName),
		typeIndex: typeIndex,
	})
	return nil
}

// addFunction registers one defined function before its body is traversed.
func (m *binaryModule) addFunction(index int) error {
	children := m.source.nodes[index].children
	if len(children) < 2 || m.source.nodeKind(children[1]) != moduleAtom {
		return m.errorf(index, "defined function needs a name")
	}
	name := m.source.nodeText(children[1])
	if !strings.HasPrefix(name, "$") {
		return m.errorf(children[1], "function name must start with `$`")
	}
	if _, exists := m.functionNames[name]; exists {
		return m.errorf(children[1], "duplicate function %s", name)
	}
	typeValue, params, locals, err := m.parseFunctionType(index)
	if err != nil {
		return err
	}
	typeIndex := m.internType(typeValue)
	functionIndex := uint32(len(m.imports) + len(m.functions))
	m.functionNames[name] = functionIndex
	m.functions = append(m.functions, binaryFunction{
		node:      index,
		name:      name,
		typeIndex: typeIndex,
		params:    params,
		locals:    locals,
	})
	return nil
}

// addFunctionExports records inline exports after every function index has
// been assigned while preserving module declaration order.
func (m *binaryModule) addFunctionExports(index int) error {
	children := m.source.nodes[index].children
	if len(children) < 2 {
		return m.errorf(index, "defined function needs a name")
	}
	functionIndex, err := m.functionIndex(children[1])
	if err != nil {
		return err
	}
	for _, child := range children[2:] {
		if m.source.listHead(child) != "export" {
			continue
		}
		exportName, err := m.inlineExportName(child)
		if err != nil {
			return err
		}
		m.exports = append(m.exports, binaryExport{
			name: string(exportName), kind: 0x00, index: functionIndex,
		})
	}
	return nil
}

// parseFunctionType reads param, result, and local declarations from a func
// list. Locals are returned separately because they are absent from the type
// section.
func (m *binaryModule) parseFunctionType(index int) (
	binaryFuncType,
	[]binaryNamedType,
	[]binaryNamedType,
	error,
) {
	if m.source.listHead(index) != "func" {
		return binaryFuncType{}, nil, nil, m.errorf(index, "expected func")
	}
	var params []binaryNamedType
	var locals []binaryNamedType
	var results []byte
	children := m.source.nodes[index].children
	for _, child := range children[1:] {
		switch m.source.listHead(child) {
		case "param":
			values, err := m.parseNamedTypes(child, true)
			if err != nil {
				return binaryFuncType{}, nil, nil, err
			}
			params = append(params, values...)
		case "result":
			values, err := m.parseNamedTypes(child, false)
			if err != nil {
				return binaryFuncType{}, nil, nil, err
			}
			for _, value := range values {
				results = append(results, value.typeCode)
			}
		case "local":
			values, err := m.parseNamedTypes(child, true)
			if err != nil {
				return binaryFuncType{}, nil, nil, err
			}
			locals = append(locals, values...)
		}
	}
	paramTypes := make([]byte, 0, len(params))
	for _, param := range params {
		paramTypes = append(paramTypes, param.typeCode)
	}
	return binaryFuncType{params: paramTypes, results: results}, params, locals, nil
}

// parseNamedTypes reads either `(param $name i32)` or an unnamed vector such
// as `(param i32 i32)`.
func (m *binaryModule) parseNamedTypes(index int, allowName bool) ([]binaryNamedType, error) {
	children := m.source.nodes[index].children
	if len(children) < 2 {
		return nil, m.errorf(index, "empty %s declaration", m.source.listHead(index))
	}
	position := 1
	name := ""
	if allowName && m.source.nodeKind(children[position]) == moduleAtom &&
		strings.HasPrefix(m.source.nodeText(children[position]), "$") {
		name = m.source.nodeText(children[position])
		position++
		if position+1 != len(children) {
			return nil, m.errorf(index, "named value must have one type")
		}
	}
	values := make([]binaryNamedType, 0, len(children)-position)
	for ; position < len(children); position++ {
		if m.source.nodeKind(children[position]) != moduleAtom {
			return nil, m.errorf(children[position], "expected value type")
		}
		typeCode, err := m.valueType(children[position])
		if err != nil {
			return nil, err
		}
		values = append(values, binaryNamedType{name: name, typeCode: typeCode})
	}
	return values, nil
}

// addTable records the single generated function-reference table.
func (m *binaryModule) addTable(index int) error {
	children := m.source.nodes[index].children
	if len(children) != 3 || m.source.nodeText(children[2]) != "funcref" {
		return m.errorf(index, "invalid table declaration")
	}
	minimum, err := m.unsigned(children[1])
	if err != nil {
		return err
	}
	m.tables = append(m.tables, binaryTable{minimum: minimum})
	return nil
}

// addMemory records one memory and any inline export.
func (m *binaryModule) addMemory(index int) error {
	children := m.source.nodes[index].children
	var minimum *uint32
	memoryIndex := uint32(len(m.memories))
	for _, child := range children[1:] {
		if m.source.listHead(child) == "export" {
			name, err := m.inlineExportName(child)
			if err != nil {
				return err
			}
			m.exports = append(m.exports, binaryExport{
				name: string(name), kind: 0x02, index: memoryIndex,
			})
			continue
		}
		if m.source.nodeKind(child) != moduleAtom || minimum != nil {
			return m.errorf(child, "invalid memory declaration")
		}
		value, err := m.unsigned(child)
		if err != nil {
			return err
		}
		minimum = &value
	}
	if minimum == nil {
		return m.errorf(index, "memory needs a minimum")
	}
	m.memories = append(m.memories, binaryMemory{minimum: *minimum})
	return nil
}

// addGlobal records one mutable or immutable numeric global.
func (m *binaryModule) addGlobal(index int) error {
	children := m.source.nodes[index].children
	if len(children) != 4 || m.source.nodeKind(children[1]) != moduleAtom ||
		m.source.nodeKind(children[3]) != moduleList {
		return m.errorf(index, "invalid global declaration")
	}
	name := m.source.nodeText(children[1])
	if !strings.HasPrefix(name, "$") {
		return m.errorf(children[1], "global name must start with `$`")
	}
	if _, exists := m.globalNames[name]; exists {
		return m.errorf(children[1], "duplicate global %s", name)
	}
	typeNode := children[2]
	mutable := false
	if m.source.nodeKind(typeNode) == moduleList && m.source.listHead(typeNode) == "mut" {
		typeChildren := m.source.nodes[typeNode].children
		if len(typeChildren) != 2 {
			return m.errorf(typeNode, "invalid mutable global type")
		}
		typeNode = typeChildren[1]
		mutable = true
	}
	typeCode, err := m.valueType(typeNode)
	if err != nil {
		return err
	}
	m.globalNames[name] = uint32(len(m.globals))
	m.globals = append(m.globals, binaryGlobal{
		name: name, typeCode: typeCode, mutable: mutable, init: children[3],
	})
	return nil
}

// addExport records a standalone function, memory, table, or global export.
func (m *binaryModule) addExport(index int) error {
	children := m.source.nodes[index].children
	if len(children) != 3 || m.source.nodeKind(children[1]) != moduleString ||
		m.source.nodeKind(children[2]) != moduleList {
		return m.errorf(index, "invalid export declaration")
	}
	name, err := m.source.stringBytes(children[1])
	if err != nil {
		return err
	}
	reference := m.source.nodes[children[2]].children
	if len(reference) != 2 || m.source.nodeKind(reference[1]) != moduleAtom {
		return m.errorf(children[2], "invalid export reference")
	}
	var kind byte
	var target uint32
	switch m.source.nodeText(reference[0]) {
	case "func":
		kind = 0x00
		target, err = m.functionIndex(reference[1])
	case "table":
		kind = 0x01
		target, err = m.unsigned(reference[1])
	case "memory":
		kind = 0x02
		target, err = m.unsigned(reference[1])
	case "global":
		kind = 0x03
		target, err = m.globalIndex(reference[1])
	default:
		err = m.errorf(children[2], "unsupported export kind")
	}
	if err != nil {
		return err
	}
	m.exports = append(m.exports, binaryExport{name: string(name), kind: kind, index: target})
	return nil
}

// addElement resolves one active table element segment.
func (m *binaryModule) addElement(index int) error {
	children := m.source.nodes[index].children
	if len(children) < 2 || m.source.nodeKind(children[1]) != moduleList {
		return m.errorf(index, "invalid element segment")
	}
	functions := make([]uint32, 0, len(children)-2)
	for _, child := range children[2:] {
		functionIndex, err := m.functionIndex(child)
		if err != nil {
			return err
		}
		functions = append(functions, functionIndex)
	}
	m.elements = append(m.elements, binaryElement{offset: children[1], functions: functions})
	return nil
}

// addData decodes one active memory data segment.
func (m *binaryModule) addData(index int) error {
	children := m.source.nodes[index].children
	if len(children) < 3 || m.source.nodeKind(children[1]) != moduleList {
		return m.errorf(index, "invalid data segment")
	}
	var data []byte
	for _, child := range children[2:] {
		if m.source.nodeKind(child) != moduleString {
			return m.errorf(child, "data segment expects byte strings")
		}
		part, err := m.source.stringBytes(child)
		if err != nil {
			return err
		}
		data = append(data, part...)
	}
	m.data = append(m.data, binaryData{offset: children[1], bytes: data})
	return nil
}

// inlineExportName reads the string from `(export "name")`.
func (m *binaryModule) inlineExportName(index int) ([]byte, error) {
	children := m.source.nodes[index].children
	if len(children) != 2 || m.source.nodeKind(children[1]) != moduleString {
		return nil, m.errorf(index, "invalid inline export")
	}
	return m.source.stringBytes(children[1])
}

// internType returns a stable structural function-type index.
func (m *binaryModule) internType(value binaryFuncType) uint32 {
	for index, existing := range m.types {
		if string(existing.params) == string(value.params) &&
			string(existing.results) == string(value.results) {
			return uint32(index)
		}
	}
	index := uint32(len(m.types))
	m.types = append(m.types, value)
	return index
}

// encode serializes sections in the canonical WebAssembly section order.
func (m *binaryModule) encode() ([]byte, error) {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	out = appendSection(out, 1, m.encodeTypes())
	out = appendSection(out, 2, m.encodeImports())
	out = appendSection(out, 3, m.encodeFunctionTypes())
	out = appendSection(out, 4, m.encodeTables())
	out = appendSection(out, 5, m.encodeMemories())
	globals, err := m.encodeGlobals()
	if err != nil {
		return nil, err
	}
	out = appendSection(out, 6, globals)
	out = appendSection(out, 7, m.encodeExports())
	elements, err := m.encodeElements()
	if err != nil {
		return nil, err
	}
	out = appendSection(out, 9, elements)
	code, err := m.encodeCode()
	if err != nil {
		return nil, err
	}
	out = appendSection(out, 10, code)
	data, err := m.encodeData()
	if err != nil {
		return nil, err
	}
	out = appendSection(out, 11, data)
	return out, nil
}

// encodeTypes serializes all explicit and inferred function signatures.
func (m *binaryModule) encodeTypes() []byte {
	if len(m.types) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.types)))
	for _, typeValue := range m.types {
		out = append(out, 0x60)
		out = appendU32(out, uint32(len(typeValue.params)))
		out = append(out, typeValue.params...)
		out = appendU32(out, uint32(len(typeValue.results)))
		out = append(out, typeValue.results...)
	}
	return out
}

// encodeImports serializes the imported function prefix of the function index
// space.
func (m *binaryModule) encodeImports() []byte {
	if len(m.imports) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.imports)))
	for _, imported := range m.imports {
		out = appendName(out, []byte(imported.module))
		out = appendName(out, []byte(imported.name))
		out = append(out, 0x00)
		out = appendU32(out, imported.typeIndex)
	}
	return out
}

// encodeFunctionTypes serializes type indices for defined functions.
func (m *binaryModule) encodeFunctionTypes() []byte {
	if len(m.functions) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.functions)))
	for _, function := range m.functions {
		out = appendU32(out, function.typeIndex)
	}
	return out
}

// encodeTables serializes generated funcref tables.
func (m *binaryModule) encodeTables() []byte {
	if len(m.tables) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.tables)))
	for _, table := range m.tables {
		out = append(out, wasmFuncRef, 0x00)
		out = appendU32(out, table.minimum)
	}
	return out
}

// encodeMemories serializes minimum-only linear-memory limits.
func (m *binaryModule) encodeMemories() []byte {
	if len(m.memories) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.memories)))
	for _, memory := range m.memories {
		out = append(out, 0x00)
		out = appendU32(out, memory.minimum)
	}
	return out
}

// encodeGlobals serializes numeric globals and their constant expressions.
func (m *binaryModule) encodeGlobals() ([]byte, error) {
	if len(m.globals) == 0 {
		return nil, nil
	}
	out := appendU32(nil, uint32(len(m.globals)))
	for _, global := range m.globals {
		out = append(out, global.typeCode)
		if global.mutable {
			out = append(out, 0x01)
		} else {
			out = append(out, 0x00)
		}
		var err error
		out, err = m.encodeConstExpression(out, global.init)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// encodeExports serializes the module's inline and standalone exports.
func (m *binaryModule) encodeExports() []byte {
	if len(m.exports) == 0 {
		return nil
	}
	out := appendU32(nil, uint32(len(m.exports)))
	for _, exported := range m.exports {
		out = appendName(out, []byte(exported.name))
		out = append(out, exported.kind)
		out = appendU32(out, exported.index)
	}
	return out
}

// encodeElements serializes active function-table initializers.
func (m *binaryModule) encodeElements() ([]byte, error) {
	if len(m.elements) == 0 {
		return nil, nil
	}
	out := appendU32(nil, uint32(len(m.elements)))
	for _, element := range m.elements {
		out = append(out, 0x00)
		var err error
		out, err = m.encodeConstExpression(out, element.offset)
		if err != nil {
			return nil, err
		}
		out = appendU32(out, uint32(len(element.functions)))
		for _, function := range element.functions {
			out = appendU32(out, function)
		}
	}
	return out, nil
}

// encodeData serializes active memory data segments.
func (m *binaryModule) encodeData() ([]byte, error) {
	if len(m.data) == 0 {
		return nil, nil
	}
	out := appendU32(nil, uint32(len(m.data)))
	for _, segment := range m.data {
		out = append(out, 0x00)
		var err error
		out, err = m.encodeConstExpression(out, segment.offset)
		if err != nil {
			return nil, err
		}
		out = appendName(out, segment.bytes)
	}
	return out, nil
}

// encodeConstExpression serializes the numeric constant expressions used by
// globals and active segments.
func (m *binaryModule) encodeConstExpression(out []byte, index int) ([]byte, error) {
	if m.source.nodeKind(index) != moduleList {
		return nil, m.errorf(index, "constant expression must be a list")
	}
	children := m.source.nodes[index].children
	if len(children) != 2 {
		return nil, m.errorf(index, "invalid constant expression")
	}
	value, err := m.signed(children[1], 64)
	if err != nil {
		return nil, err
	}
	switch m.source.listHead(index) {
	case "i32.const":
		out = append(out, 0x41)
		out = appendI32(out, int32(value))
	case "i64.const":
		out = append(out, 0x42)
		out = appendI64(out, value)
	default:
		return nil, m.errorf(index, "unsupported constant expression")
	}
	out = append(out, 0x0b)
	return out, nil
}

// valueType maps the generated numeric WAT types to binary value codes.
func (m *binaryModule) valueType(index int) (byte, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected value type")
	}
	switch m.source.nodeText(index) {
	case "i32":
		return wasmI32, nil
	case "i64":
		return wasmI64, nil
	case "f32":
		return wasmF32, nil
	case "f64":
		return wasmF64, nil
	default:
		return 0, m.errorf(index, "unsupported value type")
	}
}

// functionIndex resolves a symbolic or numeric function reference.
func (m *binaryModule) functionIndex(index int) (uint32, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected function reference")
	}
	text := m.source.nodeText(index)
	if strings.HasPrefix(text, "$") {
		value, ok := m.functionNames[text]
		if !ok {
			return 0, m.errorf(index, "unknown function %s", text)
		}
		return value, nil
	}
	return m.unsigned(index)
}

// globalIndex resolves a symbolic or numeric global reference.
func (m *binaryModule) globalIndex(index int) (uint32, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected global reference")
	}
	text := m.source.nodeText(index)
	if strings.HasPrefix(text, "$") {
		value, ok := m.globalNames[text]
		if !ok {
			return 0, m.errorf(index, "unknown global %s", text)
		}
		return value, nil
	}
	return m.unsigned(index)
}

// typeIndex resolves a symbolic or numeric type reference.
func (m *binaryModule) typeIndex(index int) (uint32, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected type reference")
	}
	text := m.source.nodeText(index)
	if strings.HasPrefix(text, "$") {
		value, ok := m.typeNames[text]
		if !ok {
			return 0, m.errorf(index, "unknown type %s", text)
		}
		return value, nil
	}
	return m.unsigned(index)
}

// unsigned parses a generated unsigned decimal immediate.
func (m *binaryModule) unsigned(index int) (uint32, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected unsigned integer")
	}
	value, err := strconv.ParseUint(m.source.nodeText(index), 10, 32)
	if err != nil {
		return 0, m.errorf(index, "invalid unsigned integer")
	}
	return uint32(value), nil
}

// signed parses a generated signed decimal immediate.
func (m *binaryModule) signed(index int, bits int) (int64, error) {
	if m.source.nodeKind(index) != moduleAtom {
		return 0, m.errorf(index, "expected signed integer")
	}
	text := m.source.nodeText(index)
	value, err := strconv.ParseInt(text, 0, bits)
	if err == nil {
		return value, nil
	}
	unsigned, unsignedErr := strconv.ParseUint(text, 0, bits)
	if unsignedErr != nil {
		return 0, m.errorf(index, "invalid signed integer %q", text)
	}
	return int64(unsigned), nil
}

// errorf reports an encoder failure against the generated WAT source span.
func (m *binaryModule) errorf(index int, format string, args ...any) error {
	node := m.source.nodes[index]
	return fmt.Errorf("wasm error: binary encoding at byte %d: %s",
		node.start, fmt.Sprintf(format, args...))
}

// appendSection appends one non-empty standard section.
func appendSection(out []byte, id byte, payload []byte) []byte {
	if len(payload) == 0 {
		return out
	}
	out = append(out, id)
	out = appendU32(out, uint32(len(payload)))
	return append(out, payload...)
}

// appendName appends a byte vector in the name/string encoding.
func appendName(out []byte, value []byte) []byte {
	out = appendU32(out, uint32(len(value)))
	return append(out, value...)
}

// appendU32 appends canonical unsigned LEB128.
func appendU32(out []byte, value uint32) []byte {
	for {
		current := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			current |= 0x80
		}
		out = append(out, current)
		if value == 0 {
			return out
		}
	}
}

// appendI32 appends canonical signed 32-bit LEB128.
func appendI32(out []byte, value int32) []byte {
	return appendSigned(out, int64(value), 32)
}

// appendI64 appends canonical signed 64-bit LEB128.
func appendI64(out []byte, value int64) []byte {
	return appendSigned(out, value, 64)
}

// appendSigned appends canonical signed LEB128 for the selected width.
func appendSigned(out []byte, value int64, bits int) []byte {
	remaining := value
	for {
		current := byte(remaining & 0x7f)
		remaining >>= 7
		done := (remaining == 0 && current&0x40 == 0) ||
			(remaining == -1 && current&0x40 != 0)
		if !done {
			current |= 0x80
		}
		out = append(out, current)
		if done {
			return out
		}
		bits -= 7
		if bits <= 0 {
			return out
		}
	}
}
