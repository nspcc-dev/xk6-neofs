package registry

import (
	"encoding/json"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

type ObjFilter struct {
	Status string
	Age    int
}

type ObjSelector struct {
	boltDB *bbolt.DB
	filter *ObjFilter
	mu     sync.Mutex
	lastId uint64
	// UTC date&time before which selector is locked for iteration or resetting.
	// This lock prevents concurrency issues when some VUs are selecting objects
	// while another VU resets the selector and attempts to select the same objects
	lockedUntil time.Time
}

// NewObjSelector creates a new instance of object selector that can iterate over
// objects in the specified registry.
func NewObjSelector(registry *ObjRegistry, filter *ObjFilter) *ObjSelector {
	objSelector := &ObjSelector{boltDB: registry.boltDB, filter: filter}
	return objSelector
}

// NextObject returns the next object from the registry that matches filter of
// the selector. NextObject only roams forward from the current position of the
// selector. If there are no objects that match the filter, then returns nil.
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

		if time.Now().UTC().Before(o.lockedUntil) {
			return nil
		}

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

		// Iterate over objects to find the next object matching the filter
		var obj ObjectInfo
		for ; keyBytes != nil; keyBytes, objBytes = c.Next() {
			if objBytes != nil {
				if err := json.Unmarshal(objBytes, &obj); err != nil {
					// Ignore malformed objects for now. Maybe it should be panic?
					continue
				}
				// If we reached an object that matches filter, stop iterating
				if o.filter.match(obj) {
					foundObj = &obj
					break
				}
			}
		}

		// Update the last key
		if keyBytes != nil {
			o.lastId = decodeId(keyBytes)
			return nil
		}

		return nil
	})
	return foundObj, err
}

// Resets object selector to start scanning objects from the beginning.
// After resetting the selector is locked for specified lockTime to prevent
// concurrency issues.
func (o *ObjSelector) Reset(lockTime int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	if time.Now().UTC().Before(o.lockedUntil) {
		return false
	}

	o.lastId = 0
	o.lockedUntil = time.Now().UTC().Add(time.Duration(lockTime) * time.Second)
	return true
}

// Count returns total number of objects that match filter of the selector.
func (o *ObjSelector) Count() (int, error) {
	var count = 0
	err := o.boltDB.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return nil
		}

		return b.ForEach(func(_, objBytes []byte) error {
			if objBytes != nil {
				var obj ObjectInfo
				if err := json.Unmarshal(objBytes, &obj); err != nil {
					// Ignore malformed objects
					return nil
				}
				if o.filter.match(obj) {
					count++
				}
			}
			return nil
		})
	})
	return count, err
}

func (f *ObjFilter) match(o ObjectInfo) bool {
	if f.Status != "" && f.Status != o.Status {
		return false
	}
	if f.Age != 0 {
		objAge := time.Now().UTC().Sub(o.CreatedAt).Seconds()
		if objAge < float64(f.Age) {
			return false
		}
	}
	return true
}
