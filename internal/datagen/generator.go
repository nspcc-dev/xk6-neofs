package datagen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
	"time"

	"github.com/dop251/goja"
	"go.k6.io/k6/js/modules"
)

type (
	// Generator stores buffer of random bytes with some tail and returns data slices with
	// an increasing offset so that we receive a different set of bytes after each call without
	// re-generation of the entire buffer from scratch:
	//
	//   [<----------size----------><-tail->]
	//   [<----------slice0-------->........]
	//   [.<----------slice1-------->.......]
	//   [..<----------slice2-------->......]
	Generator struct {
		vu     modules.VU
		size   int
		buf    []byte
		offset int
	}

	GenPayloadResponse struct {
		Payload goja.ArrayBuffer
		Hash    string
	}
)

// TailSize specifies number of extra random bytes in the buffer tail.
const TailSize = 1024

func NewGenerator(vu modules.VU, size int) Generator {
	if size < 0 {
		panic("size should not be negative")
	}
	return Generator{vu: vu, size: size, buf: nil, offset: 0}
}

func (g *Generator) GenPayload(calcHash bool) GenPayloadResponse {
	data := g.nextSlice()

	dataHash := ""
	if calcHash {
		hashBytes := sha256.Sum256(data)
		dataHash = hex.EncodeToString(hashBytes[:])
	}

	payload := g.vu.Runtime().NewArrayBuffer(data)
	return GenPayloadResponse{Payload: payload, Hash: dataHash}
}

func (g *Generator) nextSlice() []byte {
	if g.buf == nil {
		// Allocate buffer with extra tail for sliding and populate it with random bytes
		g.buf = make([]byte, g.size+TailSize)

		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
		chaCha8 := rand.NewChaCha8(seed)

		//nolint:staticcheck
		_, _ = chaCha8.Read(g.buf) // Per docs, err is always nil here
	}

	result := g.buf[g.offset : g.offset+g.size]

	// Shift the offset for the next call. If we've used our entire tail, then erase
	// the buffer so that on the next call it is regenerated anew
	g.offset++
	if g.offset >= TailSize {
		g.buf = nil
		g.offset = 0
	}

	return result
}
