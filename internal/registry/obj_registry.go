package registry

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"time"

	"go.etcd.io/bbolt"
)

type ObjRegistry struct {
	boltDB      *bbolt.DB
	objSelector *ObjSelector
}

const (
	// Indicates that an object was created, but its data wasn't verified yet
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

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func NewObjRegistry(dbFilePath string) *ObjRegistry {
	options := bbolt.Options{Timeout: 100 * time.Millisecond}
	boltDB, err := bbolt.Open(dbFilePath, os.ModePerm, &options)
	if err != nil {
		panic(err)
	}

	objSelector := ObjSelector{boltDB: boltDB, objStatus: statusCreated}

	objRepository := &ObjRegistry{boltDB: boltDB, objSelector: &objSelector}
	return objRepository
}

func (o *ObjRegistry) AddObject(cid, oid, s3Bucket, s3Key, payloadHash string) error {
	return o.boltDB.Update(func(tx *bbolt.Tx) error {
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

func (o *ObjRegistry) SetObjectStatus(id uint64, newStatus string) error {
	return o.boltDB.Update(func(tx *bbolt.Tx) error {
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

func (o *ObjRegistry) GetObjectCountInStatus(status string) (int, error) {
	var objCount = 0
	err := o.boltDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for keyBytes, objBytes := c.First(); keyBytes != nil; keyBytes, objBytes = c.Next() {
			if objBytes != nil {
				var obj ObjectInfo
				if err := json.Unmarshal(objBytes, &obj); err != nil {
					// Ignore malformed objects
					continue
				}
				if obj.Status == status {
					objCount++
				}
			}
		}
		return nil
	})
	return objCount, err
}

func (o *ObjRegistry) NextObjectToVerify() (*ObjectInfo, error) {
	return o.objSelector.NextObject()
}

func (o *ObjRegistry) Close() error {
	return o.boltDB.Close()
}

func encodeId(id uint64) []byte {
	idBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(idBytes, id)
	return idBytes
}

func decodeId(idBytes []byte) uint64 {
	return binary.BigEndian.Uint64(idBytes)
}
