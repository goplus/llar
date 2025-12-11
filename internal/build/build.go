package build

import (
	"context"

	"github.com/goplus/llar/internal/modload"
	"github.com/goplus/llar/pkgs/mod/module"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(ctx context.Context, mainModId, mainModVer string) error {
	formulas, err := modload.LoadPackages(ctx, module.Version{mainModId, mainModVer})
	if err != nil {
		return err
	}
	for _, f := range formulas {
		f.OnBuild(f.Proj)
	}
}
