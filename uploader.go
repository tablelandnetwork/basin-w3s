package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-libipfs/blocks"
	"github.com/ipfs/go-unixfsnode/data/builder"
	"github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/web3-storage/go-ucanto/core/delegation"
	"github.com/web3-storage/go-ucanto/did"
	"github.com/web3-storage/go-ucanto/principal"
	"github.com/web3-storage/go-ucanto/principal/ed25519/signer"
	"github.com/web3-storage/go-w3up/capability/storeadd"
	"github.com/web3-storage/go-w3up/capability/uploadadd"
	"github.com/web3-storage/go-w3up/client"
	"github.com/web3-storage/go-w3up/cmd/util"
	w3sdelegation "github.com/web3-storage/go-w3up/delegation"
	"golang.org/x/exp/slog"
)

// SpaceID is the id of a Web3 Storage space.
const SpaceID = "did:key:z6Mkv4YhtLqTKWis8KfLWGhUEcHFPYgH97BrCZia7xsUxMWj"

// w3s interface to make it easier to mock w3s.
type w3s interface {
	upload(cid.Cid, string) (cid.Cid, error)
}

// Uploader ...
type Uploader struct {
	w3s    w3s
	tmpDir string
}

// UploadResult ..
type UploadResult struct {
	Root  cid.Cid
	Shard cid.Cid
}

// NewUploader returns a new uploader.
func NewUploader(sk string, proofBytes []byte, tmpDir string) (*Uploader, error) {
	client, err := newW3sclient(sk, proofBytes)
	if err != nil {
		return nil, fmt.Errorf("creating new w3s client: %s", err)
	}
	return &Uploader{
		w3s:    client,
		tmpDir: tmpDir,
	}, nil
}

// Upload uploads the content of a io.Reader.
func (u *Uploader) Upload(ctx context.Context, r io.Reader) (UploadResult, error) {
	dest, err := u.saveTmp(r)
	if err != nil {
		return UploadResult{}, fmt.Errorf("failed saving into tmp: %s", err)
	}
	defer func() {
		if err := u.removeTmp(dest); err != nil {
			slog.Error("failed to remove tmp file", err)
		}
	}()

	root, err := u.createCar(ctx, dest)
	if err != nil {
		return UploadResult{}, fmt.Errorf("failed generating CAR: %s", err)
	}

	shard, err := u.w3s.upload(root, dest)
	if err != nil {
		return UploadResult{}, fmt.Errorf("failed archiving CAR: %s", err)
	}

	return UploadResult{
		Root:  root,
		Shard: shard,
	}, nil
}

func (u *Uploader) saveTmp(r io.Reader) (string, error) {
	randBytes := make([]byte, 16)
	_, _ = rand.Read(randBytes)
	dest := filepath.Join(u.tmpDir, hex.EncodeToString(randBytes))

	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("close file", err)
		}
	}()

	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}

	return dest, nil
}

func (u *Uploader) createCar(ctx context.Context, dest string) (cid.Cid, error) {
	hasher, err := multihash.GetHasher(multihash.SHA2_256)
	if err != nil {
		return cid.Cid{}, err
	}
	digest := hasher.Sum([]byte{})
	hash, err := multihash.Encode(digest, multihash.SHA2_256)
	if err != nil {
		return cid.Cid{}, err
	}
	proxyRoot := cid.NewCidV1(uint64(multicodec.DagPb), hash)

	cdest, err := blockstore.OpenReadWrite(
		fmt.Sprintf("%s.car", dest), []cid.Cid{proxyRoot}, []car.Option{blockstore.WriteAsCarV1(true)}...,
	)
	if err != nil {
		return cid.Cid{}, err
	}

	// Write the unixfs blocks into the store.
	root, _, err := writeFile(ctx, cdest, dest)
	if err != nil {
		return cid.Cid{}, err
	}

	if err := cdest.Finalize(); err != nil {
		return cid.Cid{}, err
	}
	// re-open/finalize with the final root.
	if err := car.ReplaceRootsInFile(fmt.Sprintf("%s.car", dest), []cid.Cid{root}); err != nil {
		return cid.Cid{}, err
	}

	return root, nil
}

func (*Uploader) removeTmp(dest string) error {
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("failed to remove file: %s", err)
	}

	if err := os.Remove(fmt.Sprintf("%s.car", dest)); err != nil {
		return fmt.Errorf("failed to remove car file: %s", err)
	}

	return nil
}

func writeFile(ctx context.Context, bs *blockstore.ReadWrite, path string) (cid.Cid, uint64, error) {
	ls := cidlink.DefaultLinkSystem()
	ls.TrustedStorage = true
	ls.StorageReadOpener = func(_ ipld.LinkContext, l ipld.Link) (io.Reader, error) {
		cl, ok := l.(cidlink.Link)
		if !ok {
			return nil, fmt.Errorf("not a cidlink")
		}
		blk, err := bs.Get(ctx, cl.Cid)
		if err != nil {
			return nil, err
		}
		return bytes.NewBuffer(blk.RawData()), nil
	}
	ls.StorageWriteOpener = func(_ ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := bytes.NewBuffer(nil)
		return buf, func(l ipld.Link) error {
			cl, ok := l.(cidlink.Link)
			if !ok {
				return fmt.Errorf("not a cidlink")
			}
			blk, err := blocks.NewBlockWithCid(buf.Bytes(), cl.Cid)
			if err != nil {
				return fmt.Errorf("new block with cid: %s", err)
			}
			if err := bs.Put(ctx, blk); err != nil {
				return fmt.Errorf("put: %s", err)
			}
			return nil
		}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return cid.Undef, 0, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("close file", err)
		}
	}()

	l, size, err := builder.BuildUnixFSFile(f, "", &ls)
	if err != nil {
		return cid.Undef, 0, err
	}

	rcl, ok := l.(cidlink.Link)
	if !ok {
		return cid.Undef, 0, fmt.Errorf("could not interpret %s", l)
	}
	return rcl.Cid, size, nil
}

type w3sclient struct {
	space  did.DID
	issuer principal.Signer
	proof  delegation.Delegation
}

func newW3sclient(sk string, proofBytes []byte) (*w3sclient, error) {
	// private key to sign UCAN invocations with
	issuer, err := signer.Parse(sk)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %s", err)
	}

	// UCAN proof(s) that the signer can perform tasks in this space (a delegation chain)
	proof, err := w3sdelegation.ExtractProof(proofBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract proof: %s", err)
	}

	space, err := did.Parse(SpaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse space id: %s", err)
	}

	return &w3sclient{
		issuer: issuer,
		proof:  proof,
		space:  space,
	}, nil
}

func (c *w3sclient) upload(root cid.Cid, dest string) (cid.Cid, error) {
	// no need to close the file here, because the http client will do.
	f, err := os.Open(fmt.Sprintf("%s.car", dest))
	if err != nil {
		return cid.Undef, err
	}

	stat, err := f.Stat()
	if err != nil {
		return cid.Undef, err
	}

	size := uint64(stat.Size())
	mh, err := multihash.SumStream(f, multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, err
	}

	shardLink := cidlink.Link{Cid: cid.NewCidV1(uint64(multicodec.Car), mh)}
	rcpt, err := client.StoreAdd(
		c.issuer,
		c.space,
		&storeadd.Caveat{Link: shardLink, Size: size},
		client.WithConnection(util.MustGetConnection()),
		client.WithProofs([]delegation.Delegation{c.proof}),
	)
	if err != nil {
		return cid.Undef, err
	}

	if rcpt.Out().Ok().Status == "upload" {
		_, err := f.Seek(0, io.SeekStart)
		if err != nil {
			return cid.Undef, err
		}

		hr, err := http.NewRequest("PUT", *rcpt.Out().Ok().Url, f)
		if err != nil {
			return cid.Undef, err
		}

		hdr := map[string][]string{}
		for k, v := range rcpt.Out().Ok().Headers.Values {
			hdr[k] = []string{v}
		}
		hr.Header = hdr
		hr.ContentLength = int64(size)
		httpClient := http.Client{
			Timeout: 0,
		}
		res, err := httpClient.Do(hr)
		if err != nil {
			return cid.Undef, err
		}

		if res.StatusCode != 200 {
			return cid.Undef, fmt.Errorf("status code: %d", res.StatusCode)
		}

		if err := res.Body.Close(); err != nil {
			return cid.Undef, fmt.Errorf("closing request body: %s", err)
		}
	}

	rcpt2, err := client.UploadAdd(
		c.issuer,
		c.space,
		&uploadadd.Caveat{Root: cidlink.Link{Cid: root}, Shards: []datamodel.Link{shardLink}},
		client.WithConnection(util.MustGetConnection()),
		client.WithProofs([]delegation.Delegation{c.proof}),
	)
	if err != nil {
		return cid.Undef, err
	}

	if rcpt2.Out().Error() != nil {
		return cid.Undef, fmt.Errorf(rcpt2.Out().Error().Message)
	}

	return shardLink.Cid, nil
}
