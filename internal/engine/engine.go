package engine

import (
	"github.com/goplus/ixgo"
	"github.com/goplus/llar/internal/loader"
	"github.com/goplus/llar/pkgs/mod/module"
)

type Engine struct {
	ctx    *ixgo.Context
	loader loader.Loader
}

func NewEngine() *Engine {
	ctx := ixgo.NewContext(ixgo.SupportMultipleInterp)
	return &Engine{ctx: ctx, loader: loader.NewFormulaLoader(ctx)}
}

func (e *Engine) Exec(task *Task) error {
}

type Task struct {
}

func NewTask(mod module.Version) *Task {
	return &Task{}
}

func (e *Task) Resolve() error {

}

func (e *Task) Build() error {

}
