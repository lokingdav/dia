# denseid-servers
Server implementations for DenseID

```bash
protoc --proto_path=api/proto \
    --go_out=api/go --go_opt=paths=source_relative \
    --go-grpc_out=api/go --go-grpc_opt=paths=source_relative \
    api/proto/enrollment/v1/enrollment.proto
```