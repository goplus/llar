package build

import (
	"context"

	"github.com/goplus/llar/internal/modload"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(ctx context.Context, modId, modVer string) error {
	modload.LoadPackages(ctx)
}
