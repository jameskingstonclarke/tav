package src

import (
	"fmt"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// implements visitor
type Generator struct {
	Root         *RootAST
	Module       *ir.Module
	CurrentBlock []*ir.Block
	// block to branch to if we need to break out of a block
	BreakBlock	 *ir.Block
	CurrentFn    *ir.Func
	// the generator symbol table contains only value.Value
	SymTable *SymTable
	Compiler *Compiler
	FnBlockCount uint32
}

func (Generator *Generator) PrintfProto() *ir.Func {
	f := Generator.Module.NewFunc("printf", types.I32, ir.NewParam("formatter", types.I8Ptr), ir.NewParam("value", types.I32))
	Generator.SymTable.Add("printf", NewTavType(TYPE_FN, "", 0, nil), f)
	return f
}

func (Generator *Generator) PutsProto() *ir.Func {
	f := Generator.Module.NewFunc("puts", types.I32, ir.NewParam("string", types.I8Ptr))
	Generator.SymTable.Add("puts", NewTavType(TYPE_FN, "", 0, nil), f)
	return f
}

func ValueFromType(tavType TavType, TavValue TavValue) value.Value {
	switch tavType.Type {
	case TYPE_BOOL:
		var val int64
		if TavValue.Bool == true {
			val = 1
		} else {
			val = 0
		}
		return constant.NewInt(types.I1, val)
	case TYPE_I8:
		return constant.NewInt(types.I8, TavValue.Int)
	case TYPE_I16:
		return constant.NewInt(types.I16, TavValue.Int)
	case TYPE_I32:
		return constant.NewInt(types.I32, TavValue.Int)
	case TYPE_I64:
		return constant.NewInt(types.I64, TavValue.Int)
	case TYPE_F32:
		return constant.NewFloat(types.Float, TavValue.Float)
	case TYPE_F64:
		return constant.NewFloat(types.Double, TavValue.Float)
	case TYPE_STRING:
		//return constant.NewBitCast(constant.NewPtrToInt(constant.NewCharArray(TavValue.String), types.I8Ptr), types.I8Ptr)
		return constant.NewCharArray(TavValue.String)
	}
	return nil
}

func (generator *Generator) VisitRootAST(RootAST *RootAST) interface{} {
	generator.PrintfProto()
	generator.PutsProto()
	for _, statement := range RootAST.Statements {
		statement.Visit(generator)
	}
	return nil
}

// TODO read this https://mapping-high-level-constructs-to-llvm-ir.readthedocs.io/en/latest/basic-constructs/casts.html
func (generator *Generator) VisitCastAST(CastAST *CastAST) interface{} {
	b := generator.Block()
	// if the type is a pointer, we perform a bitcast (this doesn't modify the bits in the value)
	if CastAST.TavType.Indirection > 0{
		return b.NewBitCast(CastAST.Expr.Visit(generator).(value.Value), ConvertType(CastAST.TavType, generator.SymTable))
	}

	// check which cast type we require by analysing the conversion pattern
	from := CastAST.TavType
	to   := InferType(CastAST.Expr, generator.SymTable)

	if (from.Type == TYPE_I8 ||from.Type == TYPE_I16 || from.Type == TYPE_I32 || from.Type == TYPE_I32) &&
		(to.Type == TYPE_F32 || to.Type == TYPE_F64){
		return b.NewSIToFP(CastAST.Expr.Visit(generator).(value.Value), ConvertType(CastAST.TavType, generator.SymTable))
	}

	switch CastAST.TavType.Type{
	case TYPE_I8:
		return b.NewTrunc(CastAST.Expr.Visit(generator).(value.Value), ConvertType(CastAST.TavType, generator.SymTable))
	case TYPE_F64:
		return b.NewFPExt(CastAST.Expr.Visit(generator).(value.Value), ConvertType(CastAST.TavType, generator.SymTable))
	}
	return nil
}

func (generator *Generator) VisitVarSetAST(VarSetAST *VarSetAST) interface{} {
	variable := generator.SymTable.Get(VarSetAST.Identifier.Lexme()).Value
	val := generator.Block().NewStore(VarSetAST.Value.Visit(generator).(value.Value), variable.(value.Value))
	return val
}

func (generator *Generator) VisitReturnAST(ReturnAST *ReturnAST) interface{} {
	generator.Block().NewRet(ReturnAST.Value.Visit(generator).(value.Value))
	return nil
}

func (generator *Generator) VisitBreakAST(BreakAST *BreakAST) interface{} {
	generator.Block().NewBr(generator.BreakBlock)
	return nil
}

func (generator *Generator) VisitForAST(ForAST *ForAST) interface{} {
	// first create the relevant blocks
	forCond:=generator.NewBlock(fmt.Sprintf("for_cond_%d",generator.FnBlockCount));
	forBody:=generator.NewBlock(fmt.Sprintf("for_body_%d",generator.FnBlockCount));
	forEnd:=generator.NewBlock(fmt.Sprintf("for_end_%d",generator.FnBlockCount));
	generator.PushBlock(forCond)
	// TODO gcc doesn't seem to like this
	forCond.NewCondBr(ForAST.Condition.Visit(generator).(value.Value), forBody, forEnd)
	generator.ExitBlock()


	generator.BreakBlock = forEnd

	generator.PushBlock(forBody)
	generator.SymTable.NewScope(fmt.Sprintf("for_body_%d",generator.FnBlockCount))
	ForAST.Body.Visit(generator)
	forBody.NewBr(forCond)
	generator.SymTable.PopScope()
	generator.ExitBlock()

	// branch to the for condition
	generator.Block().NewBr(forCond)
	// now we have finished the for
	generator.PushBlock(forEnd)


	return nil
}

func (generator *Generator) VisitIfAST(IfAST *IfAST) interface{} {

	// first create the relevant blocks
	ifCond:=generator.Block()
	ifBody:=generator.NewBlock(fmt.Sprintf("if_body_%d",generator.FnBlockCount));

	var elifConditions []*ir.Block
	var elifBodies 	   []*ir.Block

	for i:=0; i<len(IfAST.ElifCondition);i++{
		elifConditions = append(elifConditions,generator.NewBlock(fmt.Sprintf("elif_cond_%d_%d", i, generator.FnBlockCount)))
		elifBodies = append(elifBodies,generator.NewBlock(fmt.Sprintf("elif_body_%d_%d", i, generator.FnBlockCount)))
	}

	var elseBody *ir.Block
	if IfAST.ElseBody != nil{
		elseBody = generator.NewBlock(fmt.Sprintf("else_body_%d",generator.FnBlockCount))
	}

	end:=generator.NewBlock(fmt.Sprintf("if_end_%d", generator.FnBlockCount))

	if len(elifConditions) > 0 {
		ifCond.NewCondBr(IfAST.IfCondition.Visit(generator).(value.Value), ifBody, elifConditions[0])
	}else{
		ifCond.NewCondBr(IfAST.IfCondition.Visit(generator).(value.Value), ifBody, end)
	}
	// process if body
	generator.PushBlock(ifBody)
	generator.SymTable.NewScope(fmt.Sprintf("if_body_%d",generator.FnBlockCount))
	IfAST.IfBody.Visit(generator)
	ifBody.NewBr(end)
	generator.SymTable.PopScope()
	generator.ExitBlock()

	// process elif
	for i:=0; i<len(IfAST.ElifCondition);i++{
		generator.PushBlock(elifConditions[i])
		if i == len(IfAST.ElifCondition)-1{
			elifConditions[i].NewCondBr(IfAST.ElifCondition[i].Visit(generator).(value.Value), elifBodies[i], end)
		}else{
			elifConditions[i].NewCondBr(IfAST.ElifCondition[i].Visit(generator).(value.Value), elifBodies[i], elifConditions[i+1])
		}
		generator.ExitBlock()
		generator.SymTable.NewScope(fmt.Sprintf("elif_body_%d_%d", i, generator.FnBlockCount))
		generator.PushBlock(elifBodies[i])
		IfAST.ElifBody[i].Visit(generator)
		elifBodies[i].NewBr(end)
		generator.SymTable.PopScope()
		generator.ExitBlock()
	}

	// process else
	if elseBody != nil {
		generator.SymTable.NewScope(fmt.Sprintf("else_body_%d",generator.FnBlockCount))
		generator.PushBlock(elseBody)
		IfAST.ElseBody.Visit(generator)
		elseBody.NewBr(end)
		generator.ExitBlock()
		generator.SymTable.PopScope()
	}

	// enter the if condition
	//generator.Block().NewBr(ifCond)
	generator.PushBlock(end)
	return nil
}

func (generator *Generator) VisitStructAST(StructAST *StructAST) interface{} {

	// create a new symbol table for the struct members
	generator.SymTable.NewScope(StructAST.Identifier.Lexme() + "_members")
	// create a new symbol table containing the children
	for _, member := range StructAST.Fields {
		generator.SymTable.Add(member.Identifier.Lexme(), member.Type, nil)
	}
	generator.SymTable.PopScope()

	// add the struct to the symbol table
	s := types.NewStruct()
	generator.SymTable.Add(StructAST.Identifier.Lexme(), NewTavType(TYPE_STRUCT, "", 0, nil), s)
	s.Packed = StructAST.Packed
	for _, field := range StructAST.Fields {
		s.Fields = append(s.Fields, ConvertType(field.Type, generator.SymTable))
	}
	generator.Module.NewTypeDef(StructAST.Identifier.Lexme(), s)
	return nil
}

func (generator *Generator) VisitFnAST(FnAST *FnAST) interface{} {

	identifier := FnAST.Identifier.Lexme()

	// add each function paramater to the function body scope
	generator.SymTable.NewScope(identifier + "_body")
	var params []*ir.Param
	for _, param := range FnAST.Params {
		p := ir.NewParam(param.Identifier.Lexme(), ConvertType(param.Type, generator.SymTable))
		params = append(params, p)
		generator.SymTable.Add(param.Identifier.Lexme(), param.Type, p)
	}

	// create the function, the function body and visit the function block
	f := generator.Module.NewFunc(identifier, ConvertType(FnAST.RetType, generator.SymTable), params...)
	generator.CurrentFn = f
	b := f.NewBlock(identifier + "_body")
	generator.CurrentBlock = append(generator.CurrentBlock, b) // push the block to the stack
	for _, stmt := range FnAST.Body {
		stmt.Visit(generator)
	}
	if FnAST.RetType.Type == TYPE_VOID{
		generator.Block().NewRet(nil)
	}
	generator.CurrentBlock = generator.CurrentBlock[:len(generator.CurrentBlock)-1] // pop the block from the stack

	generator.SymTable.PopScope()
	// finally add the function to the symbol table
	generator.SymTable.Add(identifier, NewTavType(TYPE_FN, "", 0, &FnAST.RetType), f)
	return f
}

func (generator *Generator) VisitVarDefAST(VarDefAST *VarDefAST) interface{} {
	b := generator.Block()
	// allocate memory on the stack & then store the assignment
	v := b.NewAlloca(ConvertType(VarDefAST.Type, generator.SymTable))
	// if the variable assignment isn't nil, visit it and create an instruction to initialise the value
	if VarDefAST.Assignment != nil {
		assignment := VarDefAST.Assignment.Visit(generator)
		storeType := assignment.(value.Value)
		// if the type is a pointer or a struct, we have to use a load instruction before the store
		if VarDefAST.Type.Indirection != 0 || VarDefAST.Type.Type == TYPE_STRUCT {
			storeType = b.NewLoad(ConvertType(VarDefAST.Type, generator.SymTable), assignment.(value.Value))
		}
		b.NewStore(storeType, v)
	}
	// store the actual variable allocation in the symbol table
	// this will be retrieved any time we visit the variable
	generator.SymTable.Add(VarDefAST.Identifier.Lexme(), VarDefAST.Type, v)
	return nil
}

func (generator *Generator) VisitBlockAST(BlockAST *BlockAST) interface{} {
	b:=generator.NewBlock(fmt.Sprintf("block_%d",generator.FnBlockCount))
	generator.PushBlock(b)
	generator.SymTable.NewScope("block_body")
	for _, stmt := range BlockAST.Statements {
		stmt.Visit(generator)
	}
	generator.SymTable.PopScope()
	generator.ExitBlock()
	b.NewBr(generator.Block())
	return nil
}

func (generator *Generator) VisitExprSmtAST(ExprStmtAST *ExprStmtAST) interface{} {
	return ExprStmtAST.Expression.Visit(generator)
}

func (generator *Generator) VisitLiteralAST(LiteralAST *LiteralAST) interface{} {
	val := ValueFromType(LiteralAST.Type, LiteralAST.Value)
	switch val.(type){
	case *constant.CharArray:
		// https://stackoverflow.com/questions/37901866/get-pointer-to-first-element-of-array-in-llvm-ir
		//strPtr := generator.Block().NewAlloca(types.I8Ptr)
		strArr := generator.Block().NewAlloca(types.NewArray(uint64(len(LiteralAST.Value.String)), types.I8))
		// store the string constant in the string on the stack
		generator.Block().NewStore(val, strArr)
		bitcast := generator.Block().NewBitCast(strArr, types.I8Ptr)
		//generator.Block().NewStore(bitcast, strPtr)
		return bitcast
	}
	return val
}

func (generator *Generator) VisitListAST(ListAST *ListAST) interface{} {
	return nil
}

// return the value of a variable in the symbol table
// TODO This means functions are not first class variables as you cannot cast them to value.Value
func (generator *Generator) VisitVariableAST(VariableAST *VariableAST) interface{} {
	variable := generator.SymTable.Get(VariableAST.Identifier.Lexme())
	// when returning variables, we have to check the value
	// if the value is a function, we don't want to return a variable load instruction
	// instead we want to directly return the function to call
	// if the value is a paramater, we return the value directly
	switch variable.Type.Type {
	case TYPE_INSTANCE:
		return variable.Value
	case TYPE_FN:
		return variable.Value
	}
	switch variable.Value.(type) {
	case *ir.Param:
		return variable.Value
	default:
		val := generator.Block().NewLoad(ConvertType(variable.Type, generator.SymTable), variable.Value.(value.Value))
		return val
	}
}

func (generator *Generator) VisitUnaryAST(UnaryAST *UnaryAST) interface{} {
	b := generator.Block()
	right := UnaryAST.Right.Visit(generator) // if this is a variable, it is a load instruction
	// we have to invert the pointers (e.g. from *i32 -> i32 and vice versa)
	// the reason being the LLVM bindings want to see the target variable type
	// TODO multiple levels of indirection
	switch UnaryAST.Operator.Type {
	case ADDR:
		inverted := ConvertType(InvertPtrType(InferType(UnaryAST.Right, generator.SymTable), 1), generator.SymTable)
		//return	b.NewIntToPtr(right.(value.Value), inverted)
		return b.NewIntToPtr(right.(value.Value), inverted)
	case STAR:
		inverted := ConvertType(InvertPtrType(InferType(UnaryAST.Right, generator.SymTable), -1), generator.SymTable)
		return b.NewPtrToInt(right.(value.Value), inverted)
	}
	return nil
}

func (generator *Generator) VisitBinaryAST(BinaryAST *BinaryAST) interface{} {
	b := generator.Block()
	left := BinaryAST.Left.Visit(generator).(value.Value)
	right := BinaryAST.Right.Visit(generator).(value.Value)
	switch BinaryAST.Operator.Type {
	case PLUS:
		if left.Type() == types.Float {
			return b.NewFAdd(left, right)
		}
		return b.NewAdd(left, right)
	case MINUS:
		if left.Type() == types.Float {
			return b.NewFSub(left, right)
		}
		return b.NewSub(left, right)
	case STAR:
		if left.Type() == types.Float {
			return b.NewFMul(left, right)
		}
		return b.NewMul(left, right)
	case DIV:
		return b.NewFDiv(left, right)
	case EQUALS:
		return b.NewICmp(enum.IPredEQ, left, right)
	case NOT_EQUALS:
		return b.NewICmp(enum.IPredNE, left, right)
	case LESS_THAN:
		return b.NewICmp(enum.IPredSLT, left, right)
	case LESS_EQUAL:
		return b.NewICmp(enum.IPredSLE, left, right)
	case GREAT_THAN:
		return b.NewICmp(enum.IPredSGT, left, right)
	case GREAT_EQUAL:
		return b.NewICmp(enum.IPredSGE, left, right)
	}
	return nil
}

func (generator *Generator) VisitConnectiveAST(ConnectiveAST *ConnectiveAST) interface{} {
	return nil
}

func (generator *Generator) VisitCallAST(CallAST *CallAST) interface{} {
	callee := CallAST.Caller.Visit(generator)
	var args []value.Value
	for _, arg := range CallAST.Args {
		args = append(args, arg.Visit(generator).(value.Value))
	}
	return generator.Block().NewCall(callee.(value.Value), args...)
}

func (generator *Generator) VisitStructGetAST(StructGet *StructGetAST) interface{} {
	b := generator.Block()
	s := StructGet.Struct.Visit(generator)
	typeOfStruct := ConvertType(InferType(StructGet.Struct, generator.SymTable), generator.SymTable)
	member := b.NewGetElementPtr(typeOfStruct, s.(value.Value), constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 0))
	if !StructGet.Deref {
		// if we just return the member now, its a pointer! so we want to dereference it
		return b.NewPtrToInt(member, types.I32)
	}
	return member
}

func (generator *Generator) VisitStructSetAST(StructSetAST *StructSetAST) interface{} {
	b := generator.Block()
	s := StructSetAST.Struct.Visit(generator)
	typeOfStruct := ConvertType(InferType(StructSetAST.Struct, generator.SymTable), generator.SymTable)

	//offsett := CalcStructOffset(StructSetAST.Member.Lexme())

	member := b.NewGetElementPtr(typeOfStruct, s.(value.Value), constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 0))
	val := StructSetAST.Value.Visit(generator).(value.Value)
	b.NewStore(val, member)
	return member
}

// calculate the memory offset of a particular struct member
func (generator *Generator) CalcStructOffset(name, member string) int {
	// first find the struct in the symbol table
	t := generator.SymTable.Get(name).Value.(*Scope)
	for i, sym := range t.Symbols {
		if sym.Identifier == member {
			return i
		}
	}
	return 0
}

func (generator *Generator) VisitGroupAST(GroupAST *GroupAST) interface{} {
	return GroupAST.Group.Visit(generator)
}

func Generate(compiler *Compiler, RootAST *RootAST) *ir.Module {
	Log("generating")
	module := ir.NewModule()
	generator := &Generator{
		Root:     RootAST,
		Module:   module,
		SymTable: NewSymTable(),
		Compiler: compiler,
	}
	result := generator.Run()
	return result
}

func (generator *Generator) Run() *ir.Module {
	generator.Root.Visit(generator)
	return generator.Module
}

// generate a new generator scope by pushing a new symbol table scope and a new block
func (generator *Generator) NewBlock(identifier string) *ir.Block{
	block := generator.CurrentFn.NewBlock(identifier)
	generator.FnBlockCount++;
	return block
}

func (generator *Generator) PushBlock(block *ir.Block) *ir.Block{
	generator.CurrentBlock = append(generator.CurrentBlock, block)
	return block
}

func (generator *Generator) ExitBlock(){
	generator.CurrentBlock = generator.CurrentBlock[:len(generator.CurrentBlock)-1] // pop the block from the stack
}

func (generator *Generator) Block() *ir.Block{
	return generator.CurrentBlock[len(generator.CurrentBlock)-1]
}

// one block up
func (generator *Generator) PrevBlock() *ir.Block{
	return generator.CurrentBlock[len(generator.CurrentBlock)-2]
}