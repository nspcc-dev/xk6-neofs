package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

type ObjFilter struct {
	Status string
	Age    int
}

type ObjSelector struct {
	ctx       context.Context
	objChan   chan *ObjectInfo
	boltDB    *bbolt.DB
	filter    *ObjFilter
	cacheSize int
}

// objectSelectCache is the default maximum size of a batch to select from DB.
const objectSelectCache = 1000

// NewObjSelector creates a new instance of object selector that can iterate over
// objects in the specified registry.
func NewObjSelector(registry *ObjRegistry, selectionSize int, filter *ObjFilter) *ObjSelector {
	if selectionSize <= 0 {
		selectionSize = objectSelectCache
	}
	objSelector := &ObjSelector{
		ctx:       registry.ctx,
		boltDB:    registry.boltDB,
		filter:    filter,
		objChan:   make(chan *ObjectInfo, selectionSize*2),
		cacheSize: selectionSize,
	}

	go objSelector.selectLoop()

	return objSelector
}

// NextObject returns the next object from the registry that matches filter of
// the selector. NextObject only roams forward from the current position of the
// selector. If there are no objects that match the filter, blocks until one of
// the following happens:
//   - a "new" next object is available;
//   - underlying registry context is done, nil objects will be returned on the
//     currently blocked and every further NextObject calls.
func (o *ObjSelector) NextObject() *ObjectInfo {
	return <-o.objChan
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

func (o *ObjSelector) selectLoop() {
	cache := make([]*ObjectInfo, 0, o.cacheSize)
	var lastID uint64
	defer close(o.objChan)

	for {
		select {
		case <-o.ctx.Done():
			return
		default:
		}

		// cache the objects
		err := o.boltDB.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(bucketName))
			if b == nil {
				return nil
			}

			c := b.Cursor()

			// Establish the start position for searching the next object:
			// If we should go from the beginning (lastID=0), then we start
			// from the first element. Otherwise, we start from the last
			// handled ID + 1.
			var keyBytes, objBytes []byte
			if lastID == 0 {
				keyBytes, objBytes = c.First()
			} else {
				keyBytes, objBytes = c.Seek(encodeID(lastID))
				if keyBytes != nil && decodeID(keyBytes) == lastID {
					keyBytes, objBytes = c.Next()
				}
			}

			// Iterate over objects to find the next object matching the filter.
			for ; keyBytes != nil && len(cache) != o.cacheSize; keyBytes, objBytes = c.Next() {
				if objBytes != nil {
					var obj ObjectInfo
					if err := json.Unmarshal(objBytes, &obj); err != nil {
						// Ignore malformed objects for now. Maybe it should be panic?
						continue
					}

					if o.filter.match(obj) {
						cache = append(cache, &obj)
					}
				}
			}

			if len(cache) > 0 {
				lastID = cache[len(cache)-1].ID
			}

			return nil
		})
		if err != nil {
			panic(fmt.Errorf("fetching objects failed: %w", err))
		}

		for _, obj := range cache {
			select {
			case <-o.ctx.Done():
				return
			case o.objChan <- obj:
			}
		}

		if len(cache) != o.cacheSize {
			// no more objects, wait a little; the logic could be improved.
			select {
			case <-time.After(time.Second * time.Duration(o.filter.Age/2)):
			case <-o.ctx.Done():
				return
			}
		}

		// clean handled objects
		cache = cache[:0]
	}
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
