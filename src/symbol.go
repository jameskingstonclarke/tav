package src

import "hash/fnv"

const (
	SYM_MACRO  uint8 = 0x0
	SYM_STRUCT uint8 = 0x1

	// the symbol is private to the file it is declared in
	ATTRIB_PRIVATE uint8 = 0x1 << 0
	// the symbol is exposed to other files
	ATTRIB_EXPOSED uint8 = 0x1 << 1
	// the symbol is runnable at compile time (used for functions)
	ATTRIB_DOABLE uint8 = 0x1 << 2
)

// a symbol is identified by a type and an attribute
type Symbol struct {
	Type        uint8
	Attribuites uint8
}

// keep a record of symbol identifiers along with their type and attribute
type SymTable struct {
	Symbols  map[uint32]Symbol
	SymbolID map[string]uint32
	Counter  uint32
}

func (symTable *SymTable) Add(identifier string, symType, attributes uint8) {
	_, ok := symTable.SymbolID[identifier]
	Log(ok, identifier)
	Assert(!ok, "symbol already exists in symbol table", identifier)
	symTable.SymbolID[identifier] = symTable.Counter
	symTable.Symbols[symTable.Counter] = Symbol{Type: symType, Attribuites: attributes}
	symTable.Counter++
}

func (symTable *SymTable) GetID(identifier string) uint32 {
	id, ok := symTable.SymbolID[identifier]
	Assert(ok, "cannot retrieve symbol id, doesn't exist", identifier)
	return id
}

func (symTable *SymTable) Get(id uint32) Symbol {
	sym, ok := symTable.Symbols[id]
	Assert(ok, "symbol couldn't be found in the symbol table", string(id))
	return sym
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
