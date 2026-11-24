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

	"github.com/grafana/sobek"
	"github.com/nspcc-dev/neofs-sdk-go/client"
	"github.com/nspcc-dev/neofs-sdk-go/container"
	"github.com/nspcc-dev/neofs-sdk-go/container/acl"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	"github.com/nspcc-dev/neofs-sdk-go/netmap"
	"github.com/nspcc-dev/neofs-sdk-go/object"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
	"github.com/nspcc-dev/neofs-sdk-go/session/v2"
	"github.com/nspcc-dev/neofs-sdk-go/user"
	"github.com/nspcc-dev/neofs-sdk-go/version"
	"github.com/nspcc-dev/xk6-neofs/internal/stats"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type (
	Client struct {
		vu      modules.VU
		metrics *metricSet
		signer  user.Signer
		owner   user.ID
		tok     session.Token
		cli     *client.Client
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

	HeadResponse struct {
		Success bool
		Error   string
	}

	ListResponse struct {
		Success bool
		Error   string
		Count   int
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
		metrics *metricSet
		signer  user.Signer
		cli     *client.Client

		hdr     object.Object
		payload []byte
	}
)

func (c *Client) Put(containerID string, headers map[string]string, payload sobek.ArrayBuffer) PutResponse {
	cliContainerID := parseContainerID(containerID)

	attrs := make([]object.Attribute, len(headers))
	ind := 0
	for k, v := range headers {
		attrs[ind].SetKey(k)
		attrs[ind].SetValue(v)
		ind++
	}

	var o object.Object
	o.SetContainerID(cliContainerID)
	o.SetOwner(c.owner)
	o.SetAttributes(attrs...)

	resp, err := put(c.vu, c.metrics, c.cli, &c.tok, c.signer, &o, payload.Bytes())
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	return PutResponse{Success: true, ObjectID: resp.StoredObjectID().String()}
}

func (c *Client) Delete(containerID string, objectID string) DeleteResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	req := stats.Start(c.vu, c.metrics.objDeleteTotal)

	var prm client.PrmObjectDelete
	prm.WithinSessionV2(c.tok)

	_, err := c.cli.ObjectDelete(c.vu.Context(), cliContainerID, cliObjectID, c.signer, prm)
	if err != nil {
		req.Fail(c.metrics.objDeleteFails)
		return DeleteResponse{Success: false, Error: err.Error()}
	}

	req.Done(c.metrics.objDeleteDuration)
	return DeleteResponse{Success: true}
}

type sizeCounter int

func (s *sizeCounter) Write(p []byte) (int, error) {
	var l = len(p)
	*(*int)(s) += l
	return l, nil
}

func (c *Client) Get(containerID, objectID string) GetResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	req := stats.Start(c.vu, c.metrics.objGetTotal)

	var prm client.PrmObjectGet
	prm.WithinSessionV2(c.tok)
	prm.SkipChecksumVerification()

	var objSize sizeCounter
	ttfb, ttfbBody, err := get(c.vu.Context(), c.cli, cliContainerID, cliObjectID, prm, c.signer, &objSize)
	if err != nil {
		req.Fail(c.metrics.objGetFails)
		return GetResponse{Success: false, Error: err.Error()}
	}

	req.Add(c.metrics.objGetTTFB, metrics.D(ttfb))
	if ttfbBody > 0 {
		req.Add(c.metrics.objGetTTFBBody, metrics.D(ttfbBody))
	}
	req.AddDataReceived(int64(objSize))
	req.Done(c.metrics.objGetDuration)
	return GetResponse{Success: true}
}

func (c *Client) Head(containerID, objectID string) HeadResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	req := stats.Start(c.vu, c.metrics.objHeadTotal)

	var prm client.PrmObjectHead
	prm.WithinSessionV2(c.tok)
	prm.SkipChecksumVerification()

	_, err := c.cli.ObjectHead(c.vu.Context(), cliContainerID, cliObjectID, c.signer, prm)
	if err != nil {
		req.Fail(c.metrics.objHeadFails)
		return HeadResponse{Success: false, Error: err.Error()}
	}

	req.Done(c.metrics.objHeadDuration)
	return HeadResponse{Success: true}
}

func get(
	ctx context.Context,
	cli *client.Client,
	containerID cid.ID,
	objectID oid.ID,
	prm client.PrmObjectGet,
	signer user.Signer,
	w io.Writer,
) (ttfb, ttfbBody time.Duration, err error) {
	start := time.Now()

	// The first response message carries the object's header; payload
	// follows in later ones.
	_, objectReader, err := cli.ObjectGetInit(ctx, containerID, objectID, signer, prm)
	if err != nil {
		return 0, 0, err
	}
	ttfb = time.Since(start)

	body := stats.FirstByte{W: w}
	_, err = objectReader.WriteTo(&body)
	if err != nil {
		return 0, 0, err
	}

	err = objectReader.Close()
	if err != nil {
		return 0, 0, err
	}

	return ttfb, body.Since(start), nil
}

// List lists the objects in the container: those whose FilePath attribute
// starts with the prefix, or all of them if the prefix is empty. Either way
// these are root objects only, without tombstones, locks, links and the parts
// of split objects. It follows all pages of the search response, so one call
// is one complete listing.
func (c *Client) List(containerID, prefix string) ListResponse {
	cliContainerID := parseContainerID(containerID)

	var filters object.SearchFilters
	var attrs []string
	if prefix != "" {
		// Only root objects carry FilePath, so this needs no ROOT filter.
		filters.AddFilter(object.AttributeFilePath, prefix, object.MatchCommonPrefix)
		attrs = []string{object.AttributeFilePath}
	} else {
		filters.AddRootFilter()
	}

	req := stats.Start(c.vu, c.metrics.objListTotal)

	var count int
	var cursor string
	for {
		items, next, err := c.cli.SearchObjects(c.vu.Context(), cliContainerID, filters, attrs, cursor, c.signer, client.SearchObjectsOptions{})
		if err != nil {
			req.Fail(c.metrics.objListFails)
			return ListResponse{Success: false, Error: err.Error()}
		}

		count += len(items)
		if next == "" {
			break
		}
		cursor = next
	}

	req.Done(c.metrics.objListDuration)
	return ListResponse{Success: true, Count: count}
}

func (c *Client) VerifyHash(containerID, objectID, expectedHash string) VerifyHashResponse {
	cliContainerID := parseContainerID(containerID)
	cliObjectID := parseObjectID(objectID)

	var prm client.PrmObjectGet
	prm.WithinSessionV2(c.tok)

	hasher := sha256.New()
	_, _, err := get(c.vu.Context(), c.cli, cliContainerID, cliObjectID, prm, c.signer, hasher)

	if err != nil {
		return VerifyHashResponse{Success: false, Error: err.Error()}
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return VerifyHashResponse{Success: true, Error: "hash mismatch"}
	}

	return VerifyHashResponse{Success: true}
}

func (c *Client) putCnrErrorResponse(req *stats.Request, err error) PutContainerResponse {
	req.Fail(c.metrics.cnrPutFails)
	return PutContainerResponse{Success: false, Error: err.Error()}
}

func (c *Client) PutContainer(params map[string]string) PutContainerResponse {
	req := stats.Start(c.vu, c.metrics.cnrPutTotal)

	var cnr container.Container
	cnr.Init()

	cnr.SetCreationTime(time.Now())
	cnr.SetOwner(c.owner)

	if basicACLStr, ok := params["acl"]; ok {
		var basicACL acl.Basic
		err := basicACL.DecodeString(basicACLStr)
		if err != nil {
			return c.putCnrErrorResponse(req, err)
		}

		cnr.SetBasicACL(basicACL)
	}

	placementPolicyStr, ok := params["placement_policy"]
	if ok {
		var placementPolicy netmap.PlacementPolicy
		err := placementPolicy.DecodeString(placementPolicyStr)
		if err != nil {
			return c.putCnrErrorResponse(req, err)
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
			return c.putCnrErrorResponse(req, fmt.Errorf("invalid name_scope_global param: %w", err))
		}
	}

	if nameScopeGlobal {
		if !hasName {
			return c.putCnrErrorResponse(req, errors.New("you must provide container name if name_scope_global param is set"))
		}

		var domain container.Domain
		domain.SetName(containerName)

		cnr.WriteDomain(domain)
	}

	contID, err := c.cli.ContainerPut(c.vu.Context(), cnr, c.signer, client.PrmContainerPut{})
	if err != nil {
		return c.putCnrErrorResponse(req, err)
	}

	var wp waitParams
	wp.setDefaults()

	if err = c.waitForContainerPresence(c.vu.Context(), contID, &wp); err != nil {
		return c.putCnrErrorResponse(req, err)
	}

	req.Done(c.metrics.cnrPutDuration)
	return PutContainerResponse{Success: true, ContainerID: contID.EncodeToString()}
}

type Filter struct {
	Key       string
	Operation string
	Value     string
}

// Search searches for the objects in container that satisfies provided filters.
// Returns number of found objects.
func (c *Client) Search(cnrString string, filtersJS []Filter) (int, error) {
	var cID cid.ID
	err := cID.DecodeString(cnrString)
	if err != nil {
		return 0, fmt.Errorf("reading container ID: %w", err)
	}

	var op object.SearchMatchType
	var filters object.SearchFilters
	for _, flt := range filtersJS {
		if !op.DecodeString(flt.Operation) {
			return 0, fmt.Errorf("unknown filter operation: %s", flt.Operation)
		}

		filters.AddFilter(flt.Key, flt.Value, op)
	}

	var prm client.PrmObjectSearch
	prm.SetFilters(filters)

	start := time.Now()

	r, err := c.cli.ObjectSearchInit(c.vu.Context(), cID, c.signer, prm)
	if err != nil {
		return 0, fmt.Errorf("search stream initialization: %w", err)
	}
	defer func() {
		_ = r.Close()
	}()

	var objsNum int
	err = r.Iterate(func(_ oid.ID) bool {
		objsNum++
		return false
	})
	if err != nil {
		return 0, fmt.Errorf("reading search results: %w", err)
	}

	var relativeTime time.Duration
	if objsNum > 0 {
		relativeTime = time.Since(start) / time.Duration(objsNum)
	}

	stats.Report(c.vu, c.metrics.objSearchDurationRelative, metrics.D(relativeTime))

	return objsNum, nil
}

func (c *Client) Onsite(containerID string, payload sobek.ArrayBuffer) PreparedObject {
	maxObjectSize, epoch, err := parseNetworkInfo(c.vu.Context(), c.cli)
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

	obj := object.New(cliContainerID, c.owner)
	obj.SetVersion(&apiVersion)
	obj.SetType(object.TypeRegular)
	obj.SetPayloadSize(uint64(ln))
	obj.SetCreationEpoch(epoch)
	obj.SetPayloadChecksum(object.CalculatePayloadChecksum(data))

	return PreparedObject{
		vu:      c.vu,
		metrics: c.metrics,
		signer:  c.signer,
		cli:     c.cli,

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

	_, err = put(p.vu, p.metrics, p.cli, nil, p.signer, &obj, p.payload)
	if err != nil {
		return PutResponse{Success: false, Error: err.Error()}
	}

	return PutResponse{Success: true, ObjectID: id.String()}
}

func put(vu modules.VU, ms *metricSet, cli *client.Client, tok *session.Token, signer user.Signer,
	hdr *object.Object, payload []byte) (*client.ResObjectPut, error) {
	rdr := bytes.NewReader(payload)
	sz := rdr.Size()

	// starting upload
	req := stats.Start(vu, ms.objPutTotal)

	var prm client.PrmObjectPutInit
	if tok != nil {
		prm.WithinSessionV2(*tok)
	}

	objectWriter, err := cli.ObjectPutInit(vu.Context(), *hdr, signer, prm)
	if err != nil {
		req.Fail(ms.objPutFails)
		return nil, err
	}

	_, err = objectWriter.ReadFrom(rdr)
	if err != nil {
		req.Fail(ms.objPutFails)
		return nil, fmt.Errorf("copy payload chunk: %w", err)
	}

	if err = objectWriter.Close(); err != nil {
		req.Fail(ms.objPutFails)
		return nil, fmt.Errorf("writer close: %w", err)
	}

	req.AddDataSent(sz)
	req.Done(ms.objPutDuration)

	res := objectWriter.GetResult()
	return &res, err
}

func parseNetworkInfo(ctx context.Context, cli *client.Client) (maxObjSize, epoch uint64, err error) {
	ninfo, err := cli.NetworkInfo(ctx, client.PrmNetworkInfo{})
	if err != nil {
		return 0, 0, err
	}

	return ninfo.MaxObjectSize(), ninfo.CurrentEpoch(), err
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
