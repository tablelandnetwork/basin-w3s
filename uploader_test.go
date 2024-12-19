package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	_ "github.com/ipfs/go-unixfsnode/file"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/ipld/go-car/v2/storage"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// TestUploader uses a mock to test a lot of internal things that should be happening under the hood.
func TestUploader(t *testing.T) {
	client := &mockClient{t: t}
	uploader := &Uploader{
		w3s:    client,
		tmpDir: t.TempDir(),
	}

	_, err := uploader.Upload(context.Background(), strings.NewReader("Hello"))
	require.NoError(t, err)

	// check that the tmp files were removed
	_, err = os.Stat(client.dest)
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(client.dest)
	require.True(t, os.IsNotExist(err))
}

type mockClient struct {
	t    *testing.T
	dest string
}

func (c *mockClient) upload(_ cid.Cid, dest string) (cid.Cid, []ipld.Link, error) {
	c.dest = dest

	// check tmp file exists
	_, err := os.Stat(dest)
	require.NoError(c.t, err)

	// check tmp car file exists
	_, err = os.Stat(dest)
	require.NoError(c.t, err)

	// check content being uploaded
	content, err := extract(dest)
	require.NoError(c.t, err)
	require.Equal(c.t, "Hello", content)

	hash, err := multihash.Sum([]byte{}, multihash.SHA2_256, -1)
	require.NoError(c.t, err)

	cid := cid.NewCidV1(cid.Raw, hash)

	return cid, []ipld.Link{cidlink.Link{Cid: cid}}, nil
}

func extract(filename string) (string, error) {
	bs, err := blockstore.OpenReadOnly(filename)
	if err != nil {
		return "", err
	}

	carFile, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	store, err := storage.OpenReadable(carFile)
	if err != nil {
		return "", err
	}

	blkCid, err := cid.Parse(store.Roots()[0].String())
	if err != nil {
		return "", err
	}

	blk, err := bs.Get(context.Background(), blkCid)
	if err != nil {
		return "", err
	}

	return string(blk.RawData()), nil
}
