package src

type Directives struct {
	Compiler  *Compiler
	Consumer  *ParseConsumer
	SymTable  *SymTable
	NewTokens []*Token
}

func ProcessDirectives(compiler *Compiler, tokens []*Token) []*Token {
	reporter := NewReporter(compiler.Source)
	consumer := NewParseConsumer(tokens, reporter)

	directives := Directives{
		Compiler: compiler,
		Consumer: consumer,
		SymTable: NewSymTable(),
	}

	result := directives.Run()

	return result
}

func (directives *Directives) Run() []*Token {
	for !directives.Consumer.End() {
		t := directives.Consumer.Peek()
		switch t {
		default:
			directives.Compiler.Critical(directives.Consumer.Reporter, ERR_UNEXPECTED_TOKEN, "token wasn't expected")
		}
	}

	return directives.NewTokens
}
