package xk6_neofs

import (
	// In fact, xk6_neofs is a main module, but with different name. Leave a comment here to solve linter warning.
	_ "github.com/nspcc-dev/xk6-neofs/internal/datagen"
	// In fact, xk6_neofs is a main module, but with different name. Leave a comment here to solve linter warning.
	_ "github.com/nspcc-dev/xk6-neofs/internal/native"
	// In fact, xk6_neofs is a main module, but with different name. Leave a comment here to solve linter warning.
	_ "github.com/nspcc-dev/xk6-neofs/internal/registry"
	// In fact, xk6_neofs is a main module, but with different name. Leave a comment here to solve linter warning.
	_ "github.com/nspcc-dev/xk6-neofs/internal/s3"
	"go.k6.io/k6/js/modules"
)

const (
	version = "v0.1.0"
)

func init() {
	modules.Register("k6/x/neofs", &NeoFS{Version: version})
}

type NeoFS struct {
	Version string
}
