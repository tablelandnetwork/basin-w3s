# Basin Web3 Storage

A Golang service for uploading files to Web3 Storage.

## How it works?

It is an HTTP server with the following API:

```bash
POST /api/v1/upload
```

Basin Provider makes a call sending the file it wants to archive. The HTTP handler will create a CAR file and upload it to Web3 storage, returning the root CID.

## How to deploy

Merging into `main`, will deploy automatically in k8s.
