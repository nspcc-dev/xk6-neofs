package native

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-sdk-go/checksum"
	"github.com/nspcc-dev/neofs-sdk-go/client"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	"github.com/nspcc-dev/neofs-sdk-go/netmap"
	"github.com/nspcc-dev/neofs-sdk-go/object"
	"github.com/nspcc-dev/neofs-sdk-go/object/address"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
	"github.com/nspcc-dev/neofs-sdk-go/session"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	"github.com/nspcc-dev/neofs-sdk-go/version"
	"github.com/nspcc-dev/tzhash/tz"
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

	PreparedObject struct {
		vu  modules.VU
		key ecdsa.PrivateKey
		cli *client.Client

		hdr     object.Object
		payload []byte
	}
)

func (c *Client) Put(inputContainerID string, headers map[string]string, payload goja.ArrayBuffer) PutResponse {
	rdr := bytes.NewReader(payload.Bytes())
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

func (c *Client) Onsite(inputContainerID string, payload goja.ArrayBuffer) PreparedObject {
	maxObjectSize, epoch, hhDisabled, err := parseNetworkInfo(c.vu.Context(), c.cli)
	if err != nil {
		panic(err)
	}
	data := payload.Bytes()
	ln := len(data)
	if ln > int(maxObjectSize) {
		// not sure if load test needs object transformation
		// with parent-child relation; if needs, then replace
		// this code with the usage of object transformer from
		// neofs-loader or distribution.
		msg := fmt.Sprintf("payload size %d is bigger than network limit %d", ln, maxObjectSize)
		panic(msg)
	}

	var containerID cid.ID
	err = containerID.DecodeString(inputContainerID)
	if err != nil {
		panic(err)
	}

	var owner user.ID
	user.IDFromKey(&owner, c.key.PublicKey)

	apiVersion := version.Current()

	obj := object.New()
	obj.SetVersion(&apiVersion)
	obj.SetType(object.TypeRegular)
	obj.SetContainerID(containerID)
	obj.SetOwnerID(&owner)
	obj.SetPayloadSize(uint64(ln))
	obj.SetCreationEpoch(epoch)

	var sha, hh checksum.Checksum
	sha.SetSHA256(sha256.Sum256(data))
	obj.SetPayloadChecksum(sha)
	if !hhDisabled {
		hh.SetTillichZemor(tz.Sum(data))
		obj.SetPayloadHomomorphicHash(hh)
	}

	return PreparedObject{
		vu:  c.vu,
		key: c.key,
		cli: c.cli,

		hdr:     *obj,
		payload: data,
	}
}

func (p PreparedObject) Put(headers map[string]string) PutResponse {
	obj := p.hdr

	attrs := make([]object.Attribute, len(headers))
	ind := 0
	for k, v := range headers {
		attrs[ind].SetKey(k)
		attrs[ind].SetValue(v)
		ind++
	}
	obj.SetAttributes(attrs...)

	id, err := object.CalculateID(&obj)
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}
	obj.SetID(id)

	if err = object.CalculateAndSetSignature(p.key, &obj); err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	buf := make([]byte, 4*1024)
	rdr := bytes.NewReader(p.payload)

	// starting upload
	// TODO(alexvanin): factor uploading code of Put() methods
	stats.Report(p.vu, objPutTotal, 1)
	start := time.Now()

	objectWriter, err := p.cli.ObjectPutInit(p.vu.Context(), client.PrmObjectPutInit{})
	if err != nil {
		stats.Report(p.vu, objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	if !objectWriter.WriteHeader(obj) {
		stats.Report(p.vu, objPutFails, 1)
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

	_, err = objectWriter.Close()
	if err != nil {
		stats.Report(p.vu, objPutFails, 1)
		return PutResponse{Success: false, Error: err.Error()}
	}

	stats.ReportDataSent(p.vu, float64(obj.PayloadSize()))
	stats.Report(p.vu, objPutDuration, metrics.D(time.Since(start)))

	return PutResponse{Success: true, ObjectID: id.String()}
}

func parseNetworkInfo(ctx context.Context, cli *client.Client) (maxObjSize, epoch uint64, hhDisabled bool, err error) {
	ni, err := cli.NetworkInfo(ctx, client.PrmNetworkInfo{})
	if err != nil {
		return 0, 0, false, err
	}

	epoch = ni.Info().CurrentEpoch()
	err = errors.New("network configuration misses max object size value")

	ni.Info().NetworkConfig().IterateParameters(func(parameter *netmap.NetworkParameter) bool {
		switch string(parameter.Key()) {
		case "MaxObjectSize":
			buf := make([]byte, 8)
			copy(buf[:], parameter.Value())
			maxObjSize = binary.LittleEndian.Uint64(buf)
			err = nil
		case "HomomorphicHashingDisabled":
			arr := stackitem.NewByteArray(parameter.Value())
			hhDisabled, err = arr.TryBool()
			if err != nil {
				return true
			}
		}
		return false
	})
	return maxObjSize, epoch, hhDisabled, err
}
