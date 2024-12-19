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
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	ucanto_car "github.com/web3-storage/go-ucanto/core/car"
	"github.com/web3-storage/go-ucanto/core/delegation"
	"github.com/web3-storage/go-ucanto/did"
	"github.com/web3-storage/go-ucanto/principal"
	"github.com/web3-storage/go-ucanto/principal/ed25519/signer"
	"github.com/web3-storage/go-w3up/capability/storeadd"
	"github.com/web3-storage/go-w3up/capability/uploadadd"
	"github.com/web3-storage/go-w3up/car/sharding"
	"github.com/web3-storage/go-w3up/client"
	"github.com/web3-storage/go-w3up/cmd/util"
	w3sdelegation "github.com/web3-storage/go-w3up/delegation"
)

// w3s interface to make it easier to mock w3s.
type w3s interface {
	upload(cid.Cid, string) (cid.Cid, []ipld.Link, error)
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
func NewUploader(spaceID string, sk string, proofBytes []byte, tmpDir string) (*Uploader, error) {
	client, err := newW3sclient(spaceID, sk, proofBytes)
	if err != nil {
		return nil, fmt.Errorf("creating new w3s client: %s", err)
	}
	return &Uploader{
		w3s:    client,
		tmpDir: tmpDir,
	}, nil
}

// Upload uploads the content of a io.Reader.
func (u *Uploader) Upload(ctx context.Context, r io.Reader) (_ UploadResult, err error) {
	randBytes := make([]byte, 16)
	_, _ = rand.Read(randBytes)
	dest := filepath.Join(u.tmpDir, hex.EncodeToString(randBytes))
	dest = fmt.Sprintf("%s.car", dest)

	defer func() {
		if cErr := u.removeTmp(dest); err == nil {
			err = cErr
		}
	}()

	root, err := u.createCar(ctx, dest, r)
	if err != nil {
		return UploadResult{}, fmt.Errorf("failed generating CAR: %s", err)
	}

	root, shards, err := u.w3s.upload(root, dest)
	if err != nil {
		return UploadResult{}, fmt.Errorf("failed archiving CAR: %s", err)
	}

	return UploadResult{
		Root:  root,
		Shard: cid.MustParse(shards[0].String()),
	}, nil
}

func (u *Uploader) createCar(ctx context.Context, dest string, r io.Reader) (cid.Cid, error) {
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
		dest, []cid.Cid{proxyRoot}, []car.Option{blockstore.WriteAsCarV1(true)}...,
	)
	if err != nil {
		return cid.Cid{}, err
	}

	// Write the unixfs blocks into the store.
	root, _, err := writeFile(ctx, cdest, r)
	if err != nil {
		return cid.Cid{}, err
	}

	if err := cdest.Finalize(); err != nil {
		return cid.Cid{}, err
	}
	// re-open/finalize with the final root.
	if err := car.ReplaceRootsInFile(dest, []cid.Cid{root}); err != nil {
		return cid.Cid{}, err
	}

	return root, nil
}

func (*Uploader) removeTmp(dest string) error {
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("failed to remove file: %s", err)
	}
	return nil
}

func writeFile(ctx context.Context, bs *blockstore.ReadWrite, reader io.Reader) (_ cid.Cid, sz uint64, err error) {
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

	l, size, err := builder.BuildUnixFSFile(reader, "", &ls)
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

func newW3sclient(spaceID string, sk string, proofBytes []byte) (*w3sclient, error) {
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

	space, err := did.Parse(spaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse space id: %s", err)
	}

	return &w3sclient{
		issuer: issuer,
		proof:  proof,
		space:  space,
	}, nil
}

func (c *w3sclient) upload(root cid.Cid, dest string) (_ cid.Cid, _ []ipld.Link, err error) {
	// no need to close the file because the http client is doing that
	f, err := os.Open(dest)
	if err != nil {
		return cid.Undef, []ipld.Link{}, err
	}
	defer func() {
		// Close file and override return error type if it is nil.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		return cid.Undef, []ipld.Link{}, err
	}

	var shdlnks []ipld.Link

	size := uint64(stat.Size())
	if size < sharding.ShardSize {
		link, err := storeShard(c.issuer, c.space, f, []delegation.Delegation{c.proof})
		if err != nil {
			return cid.Undef, []ipld.Link{}, err
		}
		shdlnks = append(shdlnks, link)
	} else {
		_, blocks, err := ucanto_car.Decode(f)
		if err != nil {
			return cid.Undef, []ipld.Link{}, fmt.Errorf("decoding CAR: %s", err)
		}
		shds, err := sharding.NewSharder([]ipld.Link{}, blocks)
		if err != nil {
			return cid.Undef, []ipld.Link{}, fmt.Errorf("sharding CAR: %s", err)
		}

		for {
			shd, err := shds.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				return cid.Undef, []ipld.Link{}, err
			}
			link, err := storeShard(c.issuer, c.space, shd, []delegation.Delegation{c.proof})
			if err != nil {
				return cid.Undef, []ipld.Link{}, err
			}
			shdlnks = append(shdlnks, link)
		}
	}

	rcpt2, err := client.UploadAdd(
		c.issuer,
		c.space,
		&uploadadd.Caveat{Root: cidlink.Link{Cid: root}, Shards: shdlnks},
		client.WithConnection(util.MustGetConnection()),
		client.WithProofs([]delegation.Delegation{c.proof}),
	)
	if err != nil {
		return cid.Undef, []ipld.Link{}, err
	}

	if rcpt2.Out().Error() != nil {
		return cid.Undef, []ipld.Link{}, fmt.Errorf("%s", rcpt2.Out().Error().Message)
	}

	return root, shdlnks, nil
}

func storeShard(
	issuer principal.Signer, space did.DID, shard io.Reader, proofs []delegation.Delegation,
) (ipld.Link, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(shard)
	if err != nil {
		return nil, fmt.Errorf("reading CAR: %s", err)
	}

	mh, err := multihash.Sum(buf.Bytes(), multihash.SHA2_256, -1)
	if err != nil {
		return nil, fmt.Errorf("hashing CAR: %s", err)
	}

	link := cidlink.Link{Cid: cid.NewCidV1(0x0202, mh)}

	rcpt, err := client.StoreAdd(
		issuer,
		space,
		&storeadd.Caveat{
			Link: link,
			Size: uint64(buf.Len()),
		},
		client.WithConnection(util.MustGetConnection()),
		client.WithProofs(proofs),
	)
	if err != nil {
		return nil, fmt.Errorf("store/add %s: %s", link, err)
	}

	if rcpt.Out().Error() != nil {
		return nil, fmt.Errorf("%+v", rcpt.Out().Error())
	}

	if rcpt.Out().Ok().Status == "upload" {
		hr, err := http.NewRequest("PUT", *rcpt.Out().Ok().Url, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("creating HTTP request: %s", err)
		}

		hdr := map[string][]string{}
		for k, v := range rcpt.Out().Ok().Headers.Values {
			if k == "content-length" {
				continue
			}
			hdr[k] = []string{v}
		}

		hr.Header = hdr
		hr.ContentLength = int64(buf.Len())
		httpClient := http.Client{}
		res, err := httpClient.Do(hr)
		if err != nil {
			return nil, fmt.Errorf("doing HTTP request: %s", err)
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("non-200 status code while uploading file: %d", res.StatusCode)
		}
		err = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("closing request body: %s", err)
		}
	}

	return link, nil
}
