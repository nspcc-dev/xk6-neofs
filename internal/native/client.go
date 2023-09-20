package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/dop251/goja"
	"github.com/nspcc-dev/neofs-sdk-go/checksum"
	"github.com/nspcc-dev/neofs-sdk-go/client"
	"github.com/nspcc-dev/neofs-sdk-go/container"
	"github.com/nspcc-dev/neofs-sdk-go/container/acl"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	"github.com/nspcc-dev/neofs-sdk-go/netmap"
	"github.com/nspcc-dev/neofs-sdk-go/object"
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
		vu      modules.VU
		signer  user.Signer
		owner   user.ID
		tok     session.Object
		cli     *client.Client
		bufsize int
	}

	PutResponse struct {
		Success  bool
		ObjectID string
		Error    string
	}

	DeleteResponse struct {
		Success bool
		Error   string
	}

	GetResponse struct {
		Success bool
		Error   string
	}

	VerifyHashResponse struct {
		Success bool
		Error   string
	}

	PutContainerResponse struct {
		Success     bool
		ContainerID string
		Error       string
	}

	PreparedObject struct {
		vu      modules.VU
		signer  user.Signer
		cli     *client.Client
		bufsize int

		hdr     object.Object
		payload []byte
	}
)

const defaultBufferSize = 64 * 1024

func (c *Client) SetBufferSize(size int) {
	if size < 0 {
		panic("buffer size must be positive")
	}
	if size == 0 {
		c.bufsize = defaultBufferSize
	} else {
		c.bufsize = size
	}
}

func (c *Client) Put(containerID string, headers map[string]string, payload goja.ArrayBuffer) PutResponse {
	cliContainerID := parseContainerID(containerID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectPut)
	tok.BindContainer(cliContainerID)
	err := tok.Sign(c.signer)
	if err != nil {
		panic(err)
	}

	attrs := make([]object.Attribute, len(headers))
	ind := 0
	for k, v := range headers {
		attrs[ind].SetKey(k)
		attrs[ind].SetValue(v)
		ind++
	}

	var o object.Object
	o.SetContainerID(cliContainerID)
	o.SetOwnerID(&c.owner)
	o.SetAttributes(attrs...)

	resp, err := put(c.vu, c.bufsize, c.cli, &tok, c.signer, &o, payload.Bytes())
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	return PutResponse{Success: true, ObjectID: resp.StoredObjectID().String()}
}

func (c *Client) Delete(containerID string, objectID string) DeleteResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectDelete)
	tok.BindContainer(cliContainerID)
	tok.LimitByObjects(cliObjectID)
	err := tok.Sign(c.signer)
	if err != nil {
		panic(err)
	}

	stats.Report(c.vu, objDeleteTotal, 1)
	start := time.Now()

	var prm client.PrmObjectDelete
	prm.WithinSession(tok)

	_, err = c.cli.ObjectDelete(c.vu.Context(), cliContainerID, cliObjectID, c.signer, prm)
	if err != nil {
		stats.Report(c.vu, objDeleteFails, 1)
		return DeleteResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, objDeleteDuration, metrics.D(time.Since(start)))
	return DeleteResponse{Success: true}
}

func (c *Client) Get(containerID, objectID string) GetResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectGet)
	tok.BindContainer(cliContainerID)
	tok.LimitByObjects(cliObjectID)
	err := tok.Sign(c.signer)
	if err != nil {
		panic(err)
	}

	stats.Report(c.vu, objGetTotal, 1)
	start := time.Now()

	var prm client.PrmObjectGet
	prm.WithinSession(tok)

	var objSize = 0
	err = get(c.vu.Context(), c.cli, cliContainerID, cliObjectID, prm, c.signer, c.bufsize, func(data []byte) {
		objSize += len(data)
	})
	if err != nil {
		stats.Report(c.vu, objGetFails, 1)
		return GetResponse{Success: false, Error: err.Error()}
	}

	stats.Report(c.vu, objGetDuration, metrics.D(time.Since(start)))
	stats.ReportDataReceived(c.vu, float64(objSize))
	return GetResponse{Success: true}
}

func get(
	ctx context.Context,
	cli *client.Client,
	containerID cid.ID,
	objectID oid.ID,
	prm client.PrmObjectGet,
	signer user.Signer,
	bufSize int,
	onDataChunk func(chunk []byte),
) error {
	var buf = make([]byte, bufSize)

	_, objectReader, err := cli.ObjectGetInit(ctx, containerID, objectID, signer, prm)
	if err != nil {
		return err
	}

	n, _ := objectReader.Read(buf)
	for n > 0 {
		onDataChunk(buf[:n])
		n, _ = objectReader.Read(buf)
	}

	err = objectReader.Close()
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) VerifyHash(containerID, objectID, expectedHash string) VerifyHashResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	tok := c.tok
	tok.ForVerb(session.VerbObjectGet)
	tok.BindContainer(cliContainerID)
	tok.LimitByObjects(cliObjectID)
	err := tok.Sign(c.signer)
	if err != nil {
		panic(err)
	}

	var prm client.PrmObjectGet
	prm.WithinSession(tok)

	hasher := sha256.New()
	err = get(c.vu.Context(), c.cli, cliContainerID, cliObjectID, prm, c.signer, c.bufsize, func(data []byte) {
		hasher.Write(data)
	})
	if err != nil {
		return VerifyHashResponse{Success: false, Error: err.Error()}
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return VerifyHashResponse{Success: true, Error: "hash mismatch"}
	}

	return VerifyHashResponse{Success: true}
}

func (c *Client) putCnrErrorResponse(err error) PutContainerResponse {
	stats.Report(c.vu, cnrPutFails, 1)
	return PutContainerResponse{Success: false, Error: err.Error()}
}

func (c *Client) PutContainer(params map[string]string) PutContainerResponse {
	stats.Report(c.vu, cnrPutTotal, 1)

	var cnr container.Container
	cnr.Init()

	cnr.SetCreationTime(time.Now())
	cnr.SetOwner(c.owner)

	if basicACLStr, ok := params["acl"]; ok {
		var basicACL acl.Basic
		err := basicACL.DecodeString(basicACLStr)
		if err != nil {
			return c.putCnrErrorResponse(err)
		}

		cnr.SetBasicACL(basicACL)
	}

	placementPolicyStr, ok := params["placement_policy"]
	if ok {
		var placementPolicy netmap.PlacementPolicy
		err := placementPolicy.DecodeString(placementPolicyStr)
		if err != nil {
			return c.putCnrErrorResponse(err)
		}

		cnr.SetPlacementPolicy(placementPolicy)
	}

	containerName, hasName := params["name"]
	if hasName {
		cnr.SetName(containerName)
	}

	var err error
	var nameScopeGlobal bool
	if nameScopeGlobalStr, ok := params["name_scope_global"]; ok {
		if nameScopeGlobal, err = strconv.ParseBool(nameScopeGlobalStr); err != nil {
			return c.putCnrErrorResponse(fmt.Errorf("invalid name_scope_global param: %w", err))
		}
	}

	if nameScopeGlobal {
		if !hasName {
			return c.putCnrErrorResponse(errors.New("you must provide container name if name_scope_global param is set"))
		}

		var domain container.Domain
		domain.SetName(containerName)

		cnr.WriteDomain(domain)
	}

	start := time.Now()

	contID, err := c.cli.ContainerPut(c.vu.Context(), cnr, c.signer, client.PrmContainerPut{})
	if err != nil {
		return c.putCnrErrorResponse(err)
	}

	var wp waitParams
	wp.setDefaults()

	if err = c.waitForContainerPresence(c.vu.Context(), contID, &wp); err != nil {
		return c.putCnrErrorResponse(err)
	}

	stats.Report(c.vu, cnrPutDuration, metrics.D(time.Since(start)))
	return PutContainerResponse{Success: true, ContainerID: contID.EncodeToString()}
}

func (c *Client) Onsite(containerID string, payload goja.ArrayBuffer) PreparedObject {
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

	cliContainerID := parseContainerID(containerID)

	apiVersion := version.Current()

	obj := object.New()
	obj.SetVersion(&apiVersion)
	obj.SetType(object.TypeRegular)
	obj.SetContainerID(cliContainerID)
	obj.SetOwnerID(&c.owner)
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
		vu:      c.vu,
		signer:  c.signer,
		cli:     c.cli,
		bufsize: c.bufsize,

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

	id, err := obj.CalculateID()
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}
	obj.SetID(id)

	if err = obj.Sign(p.signer); err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	_, err = put(p.vu, p.bufsize, p.cli, nil, p.signer, &obj, p.payload)
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	return PutResponse{Success: true, ObjectID: id.String()}
}

func put(vu modules.VU, bufSize int, cli *client.Client, tok *session.Object, signer user.Signer,
	hdr *object.Object, payload []byte) (*client.ResObjectPut, error) {
	buf := make([]byte, bufSize)
	rdr := bytes.NewReader(payload)
	sz := rdr.Size()

	// starting upload
	stats.Report(vu, objPutTotal, 1)
	start := time.Now()

	var prm client.PrmObjectPutInit
	if tok != nil {
		prm.WithinSession(*tok)
	}

	objectWriter, err := cli.ObjectPutInit(vu.Context(), *hdr, signer, prm)
	if err != nil {
		stats.Report(vu, objPutFails, 1)
		return nil, err
	}

	_, err = io.CopyBuffer(objectWriter, rdr, buf)
	if err != nil {
		stats.Report(vu, objPutFails, 1)
		return nil, fmt.Errorf("read payload chunk: %w", err)
	}

	if err = objectWriter.Close(); err != nil {
		stats.Report(vu, objPutFails, 1)
		return nil, fmt.Errorf("writer close: %w", err)
	}

	stats.ReportDataSent(vu, float64(sz))
	stats.Report(vu, objPutDuration, metrics.D(time.Since(start)))

	res := objectWriter.GetResult()
	return &res, err
}

func parseNetworkInfo(ctx context.Context, cli *client.Client) (maxObjSize, epoch uint64, hhDisabled bool, err error) {
	ninfo, err := cli.NetworkInfo(ctx, client.PrmNetworkInfo{})
	if err != nil {
		return 0, 0, false, err
	}

	return ninfo.MaxObjectSize(), ninfo.CurrentEpoch(), ninfo.HomomorphicHashingDisabled(), err
}

type waitParams struct {
	timeout      time.Duration
	pollInterval time.Duration
}

func (x *waitParams) setDefaults() {
	x.timeout = 120 * time.Second
	x.pollInterval = 5 * time.Second
}

func (c *Client) waitForContainerPresence(ctx context.Context, cnrID cid.ID, wp *waitParams) error {
	return waitFor(ctx, wp, func(ctx context.Context) bool {
		_, err := c.cli.ContainerGet(ctx, cnrID, client.PrmContainerGet{})
		return err == nil
	})
}

func waitFor(ctx context.Context, params *waitParams, condition func(context.Context) bool) error {
	wctx, cancel := context.WithTimeout(ctx, params.timeout)
	defer cancel()
	ticker := time.NewTimer(params.pollInterval)
	defer ticker.Stop()
	wdone := wctx.Done()
	done := ctx.Done()
	for {
		select {
		case <-done:
			return ctx.Err()
		case <-wdone:
			return wctx.Err()
		case <-ticker.C:
			if condition(ctx) {
				return nil
			}
			ticker.Reset(params.pollInterval)
		}
	}
}

func parseContainerID(strContainerID string) cid.ID {
	var containerID cid.ID
	err := containerID.DecodeString(strContainerID)
	if err != nil {
		panic(err)
	}
	return containerID
}

func parseObjectID(strObjectID string) oid.ID {
	var cliObjectID oid.ID
	err := cliObjectID.DecodeString(strObjectID)
	if err != nil {
		panic(err)
	}
	return cliObjectID
}
