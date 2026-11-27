package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
	"go.k6.io/k6/js/modulestest"
)

func newTestRegistry(t testing.TB) (*Registry, *ObjRegistry, string) {
	t.Helper()

	rt := modulestest.NewRuntime(t)
	root := &RootModule{
		registries: make(map[string]*ObjRegistry),
		selectors:  make(map[string]*ObjSelector),
	}
	mod := root.NewModuleInstance(rt.VU).(*Registry)
	dbPath := filepath.Join(t.TempDir(), "registry.bolt")

	reg := mod.Open(dbPath)
	t.Cleanup(func() {
		require.NoError(t, reg.Close())
	})
	return mod, reg, dbPath
}

func insertRawBytes(t testing.TB, reg *ObjRegistry, cid, s3Bucket string, makeValue func(id uint64) ([]byte, error)) uint64 {
	t.Helper()

	var id uint64
	err := reg.boltDB.Update(func(tx *bbolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		b, err := root.CreateBucketIfNotExists(partitionKey(cid, s3Bucket))
		if err != nil {
			return err
		}
		id, err = root.NextSequence()
		if err != nil {
			return err
		}
		raw, err := makeValue(id)
		if err != nil {
			return err
		}
		return b.Put(encodeID(id), raw)
	})
	require.NoError(t, err)
	return id
}

func insertRawObject(t testing.TB, reg *ObjRegistry, obj ObjectInfo) uint64 {
	t.Helper()

	if obj.Status == "" {
		obj.Status = statusCreated
	}
	if obj.CreatedAt.IsZero() {
		obj.CreatedAt = time.Now().UTC()
	}
	return insertRawBytes(t, reg, obj.CID, obj.S3Bucket, func(id uint64) ([]byte, error) {
		obj.ID = id
		return json.Marshal(obj)
	})
}

func insertMalformedRecord(t testing.TB, reg *ObjRegistry, cid, s3Bucket string) uint64 {
	t.Helper()

	return insertRawBytes(t, reg, cid, s3Bucket, func(uint64) ([]byte, error) {
		return []byte("{not-json"), nil
	})
}

func nextWithTimeout(t testing.TB, next func() *ObjectInfo, timeout time.Duration) *ObjectInfo {
	t.Helper()

	ch := make(chan *ObjectInfo, 1)
	go func() {
		ch <- next()
	}()
	select {
	case obj := <-ch:
		return obj
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for NextObject after %s", timeout)
		return nil
	}
}

func TestObjRegistryPartitioning(t *testing.T) {
	_, reg, _ := newTestRegistry(t)

	require.NoError(t, reg.AddObject("shared", "oid-1", "", "", "hash-1"))
	require.NoError(t, reg.AddObject("", "", "shared", "key-1", "hash-2"))
	require.NoError(t, reg.AddObject("", "", "", "", "hash-3"))

	var partitions []string
	var ids []uint64
	err := reg.boltDB.View(func(tx *bbolt.Tx) error {
		root := tx.Bucket([]byte(bucketName))
		require.NotNil(t, root)
		return root.ForEachBucket(func(name []byte) error {
			partitions = append(partitions, string(name))
			b := root.Bucket(name)
			return b.ForEach(func(k, v []byte) error {
				var obj ObjectInfo
				require.NoError(t, json.Unmarshal(v, &obj))
				require.Equal(t, decodeID(k), obj.ID)
				ids = append(ids, obj.ID)
				return nil
			})
		})
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"cid:shared", "s3:shared", defaultPartition}, partitions)
	require.ElementsMatch(t, []uint64{1, 2, 3}, ids)

	// Wrong partition must not find or modify the object.
	require.ErrorIs(t, reg.SetObjectStatus("", "shared", 1, "verified"), errObjectNotFound)
	require.ErrorIs(t, reg.SetObjectStatus("missing", "", 1, "verified"), errObjectNotFound)
	require.NoError(t, reg.SetObjectStatus("shared", "", 1, "verified"))

	// DeleteObject on missing or wrong partition is a no-op.
	require.NoError(t, reg.DeleteObject("missing", "", 1))
	require.NoError(t, reg.DeleteObject("", "shared", 1))
	require.NoError(t, reg.DeleteObject("", "shared", 2))

	selCID := NewObjSelector(reg, 10, &ObjFilter{CID: "shared", Status: "verified"})
	count, err := selCID.Count()
	require.NoError(t, err)
	require.Equal(t, 1, count)

	selS3 := NewObjSelector(reg, 10, &ObjFilter{S3Bucket: "shared"})
	count, err = selS3.Count()
	require.NoError(t, err)
	require.Equal(t, 0, count)

	selAll := NewObjSelector(reg, 10, &ObjFilter{})
	count, err = selAll.Count()
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func TestObjSelectorFilterByBucketAndCID(t *testing.T) {
	mod, reg, dbPath := newTestRegistry(t)

	require.NoError(t, reg.AddObject("", "", "bucket-a", "key-a1", "hash-a1"))
	require.NoError(t, reg.AddObject("", "", "bucket-b", "key-b1", "hash-b1"))
	require.NoError(t, reg.AddObject("", "", "bucket-a", "key-a2", "hash-a2"))
	require.NoError(t, reg.AddObject("cid-1", "oid-1", "", "", "hash-c1"))
	require.NoError(t, reg.AddObject("cid-2", "oid-2", "", "", "hash-c2"))
	require.NoError(t, reg.AddObject("same", "oid-same", "", "", "hash-s1"))
	require.NoError(t, reg.AddObject("", "", "same", "key-same", "hash-s2"))

	selA := mod.GetSelector(dbPath, "s3_a", 10, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "bucket-a",
	})
	countA, err := selA.Count()
	require.NoError(t, err)
	require.Equal(t, 2, countA)

	obj1 := nextWithTimeout(t, selA.NextObject, 2*time.Second)
	require.NotNil(t, obj1)
	require.Equal(t, "bucket-a", obj1.S3Bucket)
	require.Equal(t, "key-a1", obj1.S3Key)

	obj2 := nextWithTimeout(t, selA.NextObject, 2*time.Second)
	require.NotNil(t, obj2)
	require.Equal(t, "bucket-a", obj2.S3Bucket)
	require.Equal(t, "key-a2", obj2.S3Key)

	selC1 := mod.GetSelector(dbPath, "cid_1", 10, map[string]string{
		"status": statusCreated,
		"c_id":   "cid-1",
	})
	countC1, err := selC1.Count()
	require.NoError(t, err)
	require.Equal(t, 1, countC1)

	objC1 := nextWithTimeout(t, selC1.NextObject, 2*time.Second)
	require.NotNil(t, objC1)
	require.Equal(t, "cid-1", objC1.CID)
	require.Equal(t, "oid-1", objC1.OID)

	selSameS3 := mod.GetSelector(dbPath, "same_s3", 10, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "same",
	})
	objSameS3 := nextWithTimeout(t, selSameS3.NextObject, 2*time.Second)
	require.NotNil(t, objSameS3)
	require.Equal(t, "same", objSameS3.S3Bucket)
	require.Empty(t, objSameS3.CID)

	selLate := mod.GetSelector(dbPath, "late_bucket", 10, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "bucket-late",
	})
	countLate, err := selLate.Count()
	require.NoError(t, err)
	require.Equal(t, 0, countLate)

	require.NoError(t, reg.AddObject("", "", "bucket-late", "key-late", "hash-late"))
	objLate := nextWithTimeout(t, selLate.NextObject, 2*time.Second)
	require.NotNil(t, objLate)
	require.Equal(t, "bucket-late", objLate.S3Bucket)
	require.Equal(t, "key-late", objLate.S3Key)
}

func TestObjSelectorUnfilteredAcrossPartitions(t *testing.T) {
	mod, reg, dbPath := newTestRegistry(t)

	// Populate sequentially bucket-by-bucket plus gRPC and default partitions.
	for i := range 30 {
		require.NoError(t, reg.AddObject("", "", "bucket-a", fmt.Sprintf("a-%d", i), "hash"))
	}
	for i := range 30 {
		require.NoError(t, reg.AddObject("", "", "bucket-b", fmt.Sprintf("b-%d", i), "hash"))
	}
	for i := range 5 {
		require.NoError(t, reg.AddObject("cid-1", fmt.Sprintf("o-%d", i), "", "", "hash"))
	}
	require.NoError(t, reg.AddObject("", "", "", "", "hash-default"))

	// Create an empty partition to confirm it does not stall selection.
	emptyID := insertRawObject(t, reg, ObjectInfo{S3Bucket: "bucket-empty"})
	require.NoError(t, reg.DeleteObject("", "bucket-empty", emptyID))

	const total = 66
	sel := mod.GetSelector(dbPath, "all", 30, map[string]string{
		"status": statusCreated,
	})
	count, err := sel.Count()
	require.NoError(t, err)
	require.Equal(t, total, count)

	seen := make(map[uint64]struct{}, total)
	firstBatchPartitions := make(map[string]struct{})
	for i := range total {
		obj := nextWithTimeout(t, sel.NextObject, 2*time.Second)
		require.NotNil(t, obj)
		_, dup := seen[obj.ID]
		require.False(t, dup, "duplicate object id %d", obj.ID)
		seen[obj.ID] = struct{}{}
		if i < 30 {
			firstBatchPartitions[string(partitionKey(obj.CID, obj.S3Bucket))] = struct{}{}
		}
	}
	require.Len(t, seen, total)
	require.Greater(t, len(firstBatchPartitions), 1, "first batch should mix across partitions")

	// Young-only partition must not block aged objects in another partition.
	_, regAge, _ := newTestRegistry(t)
	oldTime := time.Now().UTC().Add(-2 * time.Minute)
	insertRawObject(t, regAge, ObjectInfo{S3Bucket: "young-only", S3Key: "y1", CreatedAt: time.Now().UTC()})
	agedID := insertRawObject(t, regAge, ObjectInfo{S3Bucket: "aged", S3Key: "a1", CreatedAt: oldTime})

	selAge := NewObjSelector(regAge, 10, &ObjFilter{Status: statusCreated, Age: 60})
	count, err = selAge.Count()
	require.NoError(t, err)
	require.Equal(t, 1, count)

	got := nextWithTimeout(t, func() *ObjectInfo { return selAge.NextObject(t.Context()) }, 2*time.Second)
	require.NotNil(t, got)
	require.Equal(t, agedID, got.ID)
}

func TestGetSelectorSharesPerBucketSelector(t *testing.T) {
	mod, reg, dbPath := newTestRegistry(t)

	const perBucket = 20
	for i := range perBucket {
		require.NoError(t, reg.AddObject("", "", "bucket-a", fmt.Sprintf("a-%d", i), "hash"))
		require.NoError(t, reg.AddObject("", "", "bucket-b", fmt.Sprintf("b-%d", i), "hash"))
	}

	filterA := map[string]string{"status": statusCreated, "s3_bucket": "bucket-a"}
	selA1 := mod.GetSelector(dbPath, "obj_to_delete_bucket-a", 5, filterA)
	selA2 := mod.GetSelector(dbPath, "obj_to_delete_bucket-a", 5, filterA)
	require.Same(t, selA1.selector, selA2.selector)

	require.Panics(t, func() {
		mod.GetSelector(dbPath, "obj_to_delete_bucket-a", 5, map[string]string{
			"status":    statusCreated,
			"s3_bucket": "bucket-b",
		})
	})

	selB := mod.GetSelector(dbPath, "obj_to_delete_bucket-b", 5, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "bucket-b",
	})
	require.NotSame(t, selA1.selector, selB.selector)

	var (
		mu    sync.Mutex
		seenA = make(map[uint64]struct{}, perBucket)
		wg    sync.WaitGroup
	)
	for _, s := range []*VUSelector{selA1, selA2} {
		wg.Add(1)
		go func(sel *VUSelector) {
			defer wg.Done()
			for range perBucket / 2 {
				obj := nextWithTimeout(t, sel.NextObject, 2*time.Second)
				require.NotNil(t, obj)
				require.Equal(t, "bucket-a", obj.S3Bucket)
				mu.Lock()
				seenA[obj.ID] = struct{}{}
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	require.Len(t, seenA, perBucket)

	objB := nextWithTimeout(t, selB.NextObject, 2*time.Second)
	require.NotNil(t, objB)
	require.Equal(t, "bucket-b", objB.S3Bucket)

	// Cancelling one VU's context unblocks its waiting NextObject without closing the shared selector.
	vuCtx, vuCancel := context.WithCancel(context.Background())
	t.Cleanup(vuCancel)
	rt := modulestest.NewRuntime(t)
	rt.VU.CtxField = vuCtx
	modVU := mod.root.NewModuleInstance(rt.VU).(*Registry)
	selEmpty := modVU.GetSelector(dbPath, "obj_to_delete_empty", 5, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "empty-bucket",
	})
	cancelTimer := time.AfterFunc(50*time.Millisecond, vuCancel)
	t.Cleanup(func() { cancelTimer.Stop() })
	require.Nil(t, nextWithTimeout(t, selEmpty.NextObject, 2*time.Second))
	require.Nil(t, nextWithTimeout(t, selEmpty.NextObject, 2*time.Second))

	require.NoError(t, reg.AddObject("", "", "empty-bucket", "late-key", "hash"))
	selEmptyActive := mod.GetSelector(dbPath, "obj_to_delete_empty", 5, map[string]string{
		"status":    statusCreated,
		"s3_bucket": "empty-bucket",
	})
	got := nextWithTimeout(t, selEmptyActive.NextObject, 2*time.Second)
	require.NotNil(t, got)
	require.Equal(t, "late-key", got.S3Key)
}

func TestObjSelectorLastIDAdvancesAndAgeCutoff(t *testing.T) {
	require.Equal(t, minIdleBackoff, (&ObjSelector{filter: &ObjFilter{Age: 0}}).idleBackoff())
	require.Equal(t, minIdleBackoff, (&ObjSelector{filter: &ObjFilter{Age: 1}}).idleBackoff())
	require.Equal(t, time.Second, (&ObjSelector{filter: &ObjFilter{Age: 2}}).idleBackoff())
	require.Equal(t, 1500*time.Millisecond, (&ObjSelector{filter: &ObjFilter{Age: 3}}).idleBackoff())
	require.Equal(t, 5*time.Second, (&ObjSelector{filter: &ObjFilter{Age: 10}}).idleBackoff())

	_, reg, _ := newTestRegistry(t)
	now := time.Now().UTC()
	oldTime := now.Add(-10 * time.Second)

	insertRawObject(t, reg, ObjectInfo{S3Bucket: "bucket-a", S3Key: "verified-1", Status: "verified", CreatedAt: oldTime})
	insertMalformedRecord(t, reg, "", "bucket-a")
	agedID := insertRawObject(t, reg, ObjectInfo{S3Bucket: "bucket-a", S3Key: "aged-ok", Status: statusCreated, CreatedAt: oldTime})
	insertRawObject(t, reg, ObjectInfo{S3Bucket: "bucket-a", S3Key: "verified-2", Status: "verified", CreatedAt: oldTime})
	// Younger than Age=1 so it is excluded on the first poll and eligible shortly after.
	youngID := insertRawObject(t, reg, ObjectInfo{S3Bucket: "bucket-a", S3Key: "young-ok", Status: statusCreated, CreatedAt: time.Now().UTC()})

	sel := NewObjSelector(reg, 10, &ObjFilter{
		Status:   statusCreated,
		Age:      1,
		S3Bucket: "bucket-a",
	})

	count, err := sel.Count()
	require.NoError(t, err)
	require.Equal(t, 1, count)

	next := func() *ObjectInfo { return sel.NextObject(t.Context()) }
	first := nextWithTimeout(t, next, 2*time.Second)
	require.NotNil(t, first)
	require.Equal(t, agedID, first.ID)

	second := nextWithTimeout(t, next, 3*time.Second)
	require.NotNil(t, second)
	require.Equal(t, youngID, second.ID)
}
