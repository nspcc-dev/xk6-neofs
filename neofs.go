package xk6_neofs

import (
	_ "github.com/nspcc-dev/xk6-neofs/internal/native"
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
