package parser

import (
	"go/ast"
	"path/filepath"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"

	// make ixgo happy
	_ "github.com/goplus/llar/internal/ixgo"
)

type Parser struct {
	ctx *ixgo.Context
}

func NewParser(ctx *ixgo.Context) *Parser {
	return &Parser{ctx: ctx}
}

func (p *Parser) ParseAST(path string) (*ast.File, error) {
	xgoCtx := xgobuild.NewContext(p.ctx)

	pkg, err := xgoCtx.ParseDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	ast, err := pkg.ToAst()
	if err != nil {
		return nil, err
	}
	return ast, nil
}
