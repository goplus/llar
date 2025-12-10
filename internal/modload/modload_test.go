package modload

import (
	"context"
	"fmt"
	"testing"

	"github.com/goplus/llar/pkgs/mod/module"
)

func TestE2E(t *testing.T) {
	mods, err := LoadPackages(context.TODO(), module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"})
	if err != nil {
		t.Error(err)
		return
	}
	for _, f := range mods {
		fmt.Println(f.ID)
	}
}
