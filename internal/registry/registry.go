package registry

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.etcd.io/bbolt"
	"go.k6.io/k6/js/modules"
)

// RootModule is the global module object type. It is instantiated once per test
// run and will be used to create k6/x/neofs/registry module instances for each VU.
type RootModule struct {
	boltDB      *bbolt.DB
	objSelector *ObjSelector
}

// Registry represents an instance of the module for every VU.
type Registry struct {
	vu   modules.VU
	root *RootModule
}

const (
	// Indicates that an object was created, but it's data wasn't verified yet
	statusCreated = "created"
)

const bucketName = "_object"

// ObjectInfo represents information about neoFS object that has been created
// via gRPC/HTTP/S3 API.
type ObjectInfo struct {
	Id          uint64 // Identifier in bolt DB
	CID         string // Container ID in gRPC/HTTP
	OID         string // Object ID in gRPC/HTTP
	S3Bucket    string // Bucket name in S3
	S3Key       string // Object key in S3
	Status      string // Status of the object
	PayloadHash string // SHA256 hash of object payload that can be used for verification
}

// Ensure the interfaces are implemented correctly.
var (
	_ modules.Instance = &Registry{}
	_ modules.Module   = &RootModule{}
)

func init() {
	// TODO: research on a way to use configurable database name
	// TODO: research on a way to close DB properly (maybe in teardown)
	options := bbolt.Options{Timeout: 100 * time.Millisecond}
	boltDB, err := bbolt.Open("registry.bolt", os.ModePerm, &options)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Selector that searches objects for verification
	objSelector := ObjSelector{boltDB: boltDB, objStatus: statusCreated}

	rootModule := &RootModule{boltDB: boltDB, objSelector: &objSelector}
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

func (r *Registry) AddObject(cid, oid, s3Bucket, s3Key, payloadHash string) error {
	return r.root.boltDB.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}

		id, err := b.NextSequence()
		if err != nil {
			return err
		}

		object := ObjectInfo{
			Id:          id,
			CID:         cid,
			OID:         oid,
			S3Bucket:    s3Bucket,
			S3Key:       s3Key,
			PayloadHash: payloadHash,
			Status:      statusCreated,
		}
		objectJson, err := json.Marshal(object)
		if err != nil {
			return err
		}

		return b.Put(encodeId(id), objectJson)
	})
}

func (r *Registry) SetObjectStatus(id uint64, newStatus string) error {
	return r.root.boltDB.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}

		objBytes := b.Get(encodeId(id))
		if objBytes == nil {
			return nil
		}

		obj := new(ObjectInfo)
		if err := json.Unmarshal(objBytes, &obj); err != nil {
			return err
		}
		obj.Status = newStatus

		objBytes, err = json.Marshal(obj)
		if err != nil {
			return err
		}
		return b.Put(encodeId(id), objBytes)
	})
}

func (r *Registry) NextObjectToVerify() (*ObjectInfo, error) {
	return r.root.objSelector.NextObject()
}

func encodeId(id uint64) []byte {
	idBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(idBytes, id)
	return idBytes
}

func decodeId(idBytes []byte) uint64 {
	return binary.BigEndian.Uint64(idBytes)
}
