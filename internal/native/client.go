package native

import (
	"bytes"
	"crypto/ecdsa"
	"time"

	"github.com/nspcc-dev/neofs-sdk-go/client"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	"github.com/nspcc-dev/neofs-sdk-go/object"
	"github.com/nspcc-dev/neofs-sdk-go/object/address"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
	"github.com/nspcc-dev/neofs-sdk-go/session"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	"github.com/nspcc-dev/xk6-neofs/internal/stats"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type (
	Client struct {
		vu  modules.VU
		key ecdsa.PrivateKey
		tok session.Object
		cli *client.Client
	}

	PutResponse struct {
		Success  bool
		ObjectID string
		Error    string
	}

	GetResponse struct {
		Success bool
		Error   string
	}
)

func (c *Client) Put(inputContainerID string, headers map[string]string, payload []byte) PutResponse {
	rdr := bytes.NewReader(payload)
	sz := rdr.Size()

	// preparation stage
	var containerID cid.ID
	err := containerID.DecodeString(inputContainerID)
	if err != nil {
		panic(err)
	}

	var addr address.Address
	addr.SetContainerID(containerID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectPut)
	tok.ApplyTo(addr)
	err = tok.Sign(c.key)
	if err != nil {
		panic(err)
	}

	var owner user.ID
	user.IDFromKey(&owner, c.key.PublicKey)

	attrs := make([]object.Attribute, len(headers))
	ind := 0
	for k, v := range headers {
		attrs[ind].SetKey(k)
		attrs[ind].SetValue(v)
		ind++
	}

	var o object.Object
	o.SetContainerID(containerID)
	o.SetOwnerID(&owner)
	o.SetAttributes(attrs...)

	buf := make([]byte, 4*1024)

	// starting upload
	stats.Report(c.vu, objPutTotal, 1)
	start := time.Now()

	objectWriter, err := c.cli.ObjectPutInit(c.vu.Context(), client.PrmObjectPutInit{})
	if err != nil {
		stats.Report(c.vu, objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	objectWriter.WithinSession(tok)

	if !objectWriter.WriteHeader(o) {
		stats.Report(c.vu, objPutFails, 1)
		_, err := objectWriter.Close()
		return PutResponse{Success: false, Error: err.Error()}
	}

	n, _ := rdr.Read(buf)
	for n > 0 {
		if !objectWriter.WritePayloadChunk(buf[:n]) {
			break
		}
		n, _ = rdr.Read(buf)
	}

	resp, err := objectWriter.Close()
	if err != nil {
		stats.Report(c.vu, objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	stats.ReportDataSent(c.vu, float64(sz))
	stats.Report(c.vu, objPutDuration, metrics.D(time.Since(start)))

	var id oid.ID
	resp.ReadStoredObjectID(&id)

	return PutResponse{Success: true, ObjectID: id.String()}
}

func (c *Client) Get(inputContainerID, inputObjectID string) GetResponse {
	var (
		buf = make([]byte, 4*1024)
		sz  int
	)

	var containerID cid.ID
	err := containerID.DecodeString(inputContainerID)
	if err != nil {
		panic(err)
	}

	var objectID oid.ID
	err = objectID.DecodeString(inputObjectID)
	if err != nil {
		panic(err)
	}

	var addr address.Address
	addr.SetContainerID(containerID)
	addr.SetObjectID(objectID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectGet)
	tok.ApplyTo(addr)
	err = tok.Sign(c.key)
	if err != nil {
		panic(err)
	}

	stats.Report(c.vu, objGetTotal, 1)
	start := time.Now()

	var prmObjectGetInit client.PrmObjectGet
	prmObjectGetInit.ByID(objectID)
	prmObjectGetInit.FromContainer(containerID)
	prmObjectGetInit.WithinSession(tok)

	objectReader, err := c.cli.ObjectGetInit(c.vu.Context(), prmObjectGetInit)
	if err != nil {
		stats.Report(c.vu, objGetFails, 1)
		return GetResponse{Success: false, Error: err.Error()}
	}

	var o object.Object
	if !objectReader.ReadHeader(&o) {
		stats.Report(c.vu, objGetFails, 1)
		_, err := objectReader.Close()
		return GetResponse{Success: false, Error: err.Error()}
	}

	n, _ := objectReader.Read(buf)
	for n > 0 {
		sz += n
		n, _ = objectReader.Read(buf)
	}

	_, err = objectReader.Close()
	if err != nil {
		stats.Report(c.vu, objGetFails, 1)
		return GetResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, objGetDuration, metrics.D(time.Since(start)))
	stats.ReportDataReceived(c.vu, float64(sz))
	return GetResponse{Success: true}
}
