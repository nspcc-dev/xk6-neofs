package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

	"go.etcd.io/bbolt"
)

type ObjFilter struct {
	Status   string
	Age      int
	CID      string
	S3Bucket string
}

type ObjSelector struct {
	ctx       context.Context
	objChan   chan *ObjectInfo
	boltDB    *bbolt.DB
	filter    *ObjFilter
	cacheSize int
}

type bucketScan struct {
	name    string
	bucket  *bbolt.Bucket
	cursor  *bbolt.Cursor
	started bool
}

const (
	// objectSelectCache is the default maximum size of a batch to select from DB.
	objectSelectCache = 1000
	// minIdleBackoff bounds the poll sleep when Age is 0 or 1 so selectLoop does not busy-spin.
	minIdleBackoff = 500 * time.Millisecond
)

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
//   - ctx or underlying registry context is done, in which case nil is returned.
func (o *ObjSelector) NextObject(ctx context.Context) *ObjectInfo {
	if ctx.Err() != nil {
		return nil
	}
	select {
	case obj := <-o.objChan:
		return obj
	case <-ctx.Done():
		return nil
	}
}

// Count returns total number of objects that match filter of the selector.
func (o *ObjSelector) Count() (int, error) {
	var count int
	err := o.boltDB.View(func(tx *bbolt.Tx) error {
		root := tx.Bucket([]byte(bucketName))
		if root == nil {
			return nil
		}

		now := time.Now().UTC()
		if o.filter.isTargeted() {
			if b := root.Bucket(partitionKey(o.filter.CID, o.filter.S3Bucket)); b != nil {
				count = o.countBucket(b, now)
			}
			return nil
		}

		return root.ForEachBucket(func(name []byte) error {
			if b := root.Bucket(name); b != nil {
				count += o.countBucket(b, now)
			}
			return nil
		})
	})
	return count, err
}

func (o *ObjSelector) countBucket(b *bbolt.Bucket, now time.Time) int {
	var (
		count  int
		lastID uint64
		bs     = bucketScan{bucket: b}
	)
	for {
		if _, ok := bs.nextMatching(o.filter, now, &lastID); !ok {
			return count
		}
		count++
	}
}

func (o *ObjSelector) selectLoop() {
	var (
		cache     = make([]*ObjectInfo, 0, o.cacheSize)
		lastID    uint64
		lastIDs   map[string]uint64
		targetKey []byte
	)
	if o.filter.isTargeted() {
		targetKey = partitionKey(o.filter.CID, o.filter.S3Bucket)
	} else {
		lastIDs = make(map[string]uint64)
	}
	defer close(o.objChan)

	for {
		select {
		case <-o.ctx.Done():
			return
		default:
		}

		// cache the objects
		err := o.boltDB.View(func(tx *bbolt.Tx) error {
			root := tx.Bucket([]byte(bucketName))
			if root == nil {
				return nil
			}

			now := time.Now().UTC()
			if targetKey != nil {
				b := root.Bucket(targetKey)
				if b == nil {
					return nil
				}
				bs := bucketScan{bucket: b}
				for len(cache) < o.cacheSize {
					obj, ok := bs.nextMatching(o.filter, now, &lastID)
					if !ok {
						break
					}
					cache = append(cache, obj)
				}
				return nil
			}

			var active []bucketScan
			if err := root.ForEachBucket(func(name []byte) error {
				if b := root.Bucket(name); b != nil {
					active = append(active, bucketScan{
						name:   string(name),
						bucket: b,
					})
				}
				return nil
			}); err != nil {
				return err
			}

			for len(cache) < o.cacheSize && len(active) > 0 {
				idx := rand.IntN(len(active))
				bs := &active[idx]
				id := lastIDs[bs.name]
				obj, ok := bs.nextMatching(o.filter, now, &id)
				if id != 0 {
					lastIDs[bs.name] = id
				}
				if ok {
					cache = append(cache, obj)
					continue
				}
				active[idx] = active[len(active)-1]
				active = active[:len(active)-1]
			}

			return nil
		})
		if err != nil {
			if o.ctx.Err() != nil {
				return
			}
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
			// no more objects, wait before polling again
			select {
			case <-time.After(o.idleBackoff()):
			case <-o.ctx.Done():
				return
			}
		}

		// clean handled objects
		cache = cache[:0]
	}
}

func (o *ObjSelector) idleBackoff() time.Duration {
	return max(time.Duration(o.filter.Age)*time.Second/2, minIdleBackoff)
}

func (bs *bucketScan) nextMatching(filter *ObjFilter, now time.Time, lastID *uint64) (*ObjectInfo, bool) {
	var keyBytes, objBytes []byte
	for {
		if !bs.started {
			bs.cursor = bs.bucket.Cursor()
			keyBytes, objBytes = seekAfter(bs.cursor, *lastID)
			bs.started = true
		} else {
			keyBytes, objBytes = bs.cursor.Next()
		}
		if keyBytes == nil {
			return nil, false
		}

		var obj ObjectInfo
		if err := json.Unmarshal(objBytes, &obj); err != nil {
			*lastID = decodeID(keyBytes)
			continue
		}

		if !filter.isAged(obj, now) {
			return nil, false
		}

		*lastID = decodeID(keyBytes)
		if filter.matchMetadata(obj) {
			return &obj, true
		}
	}
}

func seekAfter(c *bbolt.Cursor, lastID uint64) (keyBytes, objBytes []byte) {
	if lastID == 0 {
		return c.First()
	}
	keyBytes, objBytes = c.Seek(encodeID(lastID))
	if keyBytes != nil && decodeID(keyBytes) == lastID {
		return c.Next()
	}
	return keyBytes, objBytes
}

func (f *ObjFilter) isTargeted() bool {
	return f.CID != "" || f.S3Bucket != ""
}

func (f *ObjFilter) isAged(o ObjectInfo, now time.Time) bool {
	if f.Age <= 0 {
		return true
	}
	return now.Sub(o.CreatedAt) >= time.Duration(f.Age)*time.Second
}

func (f *ObjFilter) matchMetadata(o ObjectInfo) bool {
	if f.Status != "" && f.Status != o.Status {
		return false
	}
	if f.CID != "" && f.CID != o.CID {
		return false
	}
	if f.S3Bucket != "" && f.S3Bucket != o.S3Bucket {
		return false
	}
	return true
}
