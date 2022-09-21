package registry

import (
	"sync"

	"go.k6.io/k6/js/modules"
)

// RootModule is the global module object type. It is instantiated once per test
// run and will be used to create k6/x/neofs/registry module instances for each VU.
type RootModule struct {
	// Stores object registry by path of database file. We should have only single instance
	// of registry per each file
	registries map[string]*ObjRegistry
	// Mutex to sync access to repositories map
	mu sync.Mutex
}

// Registry represents an instance of the module for every VU.
type Registry struct {
	vu   modules.VU
	root *RootModule
}

// Ensure the interfaces are implemented correctly.
var (
	_ modules.Instance = &Registry{}
	_ modules.Module   = &RootModule{}
)

func init() {
	rootModule := &RootModule{registries: make(map[string]*ObjRegistry)}
	modules.Register("k6/x/neofs/registry", rootModule)
}

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func (r *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &Registry{vu: vu, root: r}
	return mi
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (r *Registry) Exports() modules.Exports {
	return modules.Exports{Default: r}
}

// Open creates a new instance of object registry that will store information about objects
// in the specified file. If repository instance for the file was previously created, then
// Open will return the existing instance of repository, because bolt database allows only
// one write connection at a time
func (r *Registry) Open(dbFilePath string) *ObjRegistry {
	r.root.mu.Lock()
	defer r.root.mu.Unlock()

	registry := r.root.registries[dbFilePath]
	if registry == nil {
		registry = NewObjRegistry(dbFilePath)
		r.root.registries[dbFilePath] = registry
	}
	return registry
}
