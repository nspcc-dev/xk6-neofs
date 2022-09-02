package registry

import (
	"encoding/json"
	"sync"

	"go.etcd.io/bbolt"
)

type ObjSelector struct {
	boltDB    *bbolt.DB
	mu        sync.Mutex
	lastId    uint64
	objStatus string
}

func (o *ObjSelector) NextObject() (*ObjectInfo, error) {
	var foundObj *ObjectInfo
	err := o.boltDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}

		c := b.Cursor()

		// We use mutex so that multiple VUs won't attempt to modify lastId simultaneously
		// TODO: consider singleton channel that will produce those ids on demand
		o.mu.Lock()
		defer o.mu.Unlock()

		// Establish the start position for searching the next object:
		// If we should go from the beginning (lastId=0), then we start from the first element
		// Otherwise we start from the key right after the lastId
		var keyBytes, objBytes []byte
		if o.lastId == 0 {
			keyBytes, objBytes = c.First()
		} else {
			c.Seek(encodeId(o.lastId))
			keyBytes, objBytes = c.Next()
		}

		// Iterate over objects to find the next object in the target status
		var obj ObjectInfo
		for ; keyBytes != nil; keyBytes, objBytes = c.Next() {
			if objBytes != nil {
				if err := json.Unmarshal(objBytes, &obj); err != nil {
					// Ignore malformed objects for now. Maybe it should be panic?
					continue
				}
				// If we reached an object in the target status, stop iterating
				if obj.Status == o.objStatus {
					foundObj = &obj
					break
				}
			}
		}

		// Update the last key
		if keyBytes != nil {
			o.lastId = decodeId(keyBytes)
		} else {
			// Loopback to beginning so that we can revisit objects which were taken for verification
			// but their status wasn't changed
			// TODO: stop looping back to beginning too quickly
			o.lastId = 0
		}

		return nil
	})
	return foundObj, err
}
